// Command report_illustrations generates validated 2K report artwork from a
// local plan. It writes durable submission receipts so an uncertain paid call
// is never repeated automatically.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"service/component"
	"service/config"
)

const (
	maxPlanBytes      = 4 << 20
	maxReferenceBytes = 32 << 20
)

var safeJobID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

type illustrationJob struct {
	ID        string `json:"id"`
	Prompt    string `json:"prompt"`
	Reference string `json:"reference,omitempty"`
}

type illustrationReceipt struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	InputHash     string `json:"input_hash"`
	ReferenceHash string `json:"reference_hash,omitempty"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	MIMEType      string `json:"mime_type,omitempty"`
	ImageFile     string `json:"image_file,omitempty"`
	ImageSHA256   string `json:"image_sha256,omitempty"`
	SubmittedAt   string `json:"submitted_at"`
	CompletedAt   string `json:"completed_at,omitempty"`
}

func main() {
	planPath := flag.String("plan", "", "绘图任务 JSON 路径")
	outputDir := flag.String("output", "", "图片与收据输出目录")
	flag.Parse()
	if err := run(context.Background(), *planPath, *outputDir); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, planPath, outputDir string) error {
	if strings.TrimSpace(planPath) == "" || strings.TrimSpace(outputDir) == "" {
		return errors.New("必须指定 --plan 和 --output")
	}
	if err := config.LoadLocalEnv(); err != nil {
		return errors.New("无法加载本地环境配置")
	}
	cfg := config.Init()
	client, err := component.NewReportIllustrationClient(cfg, nil)
	if err != nil {
		return fmt.Errorf("绘图服务不可用: %w", err)
	}
	jobs, planDir, err := loadPlan(planPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return errors.New("无法创建输出目录")
	}
	for _, job := range jobs {
		if err := processJob(ctx, client, cfg, planDir, outputDir, job); err != nil {
			return fmt.Errorf("绘图任务 %s 失败: %w", job.ID, err)
		}
	}
	return nil
}

func loadPlan(planPath string) ([]illustrationJob, string, error) {
	f, err := os.Open(planPath)
	if err != nil {
		return nil, "", errors.New("无法读取绘图计划")
	}
	defer f.Close()
	data, err := readLimited(f, maxPlanBytes)
	if err != nil {
		return nil, "", errors.New("绘图计划过大或无法读取")
	}
	var jobs []illustrationJob
	if err := json.Unmarshal(data, &jobs); err != nil || len(jobs) == 0 {
		return nil, "", errors.New("绘图计划必须是非空 jobs 数组")
	}
	seen := make(map[string]struct{}, len(jobs))
	for i := range jobs {
		jobs[i].ID = strings.TrimSpace(jobs[i].ID)
		jobs[i].Prompt = strings.TrimSpace(jobs[i].Prompt)
		jobs[i].Reference = strings.TrimSpace(jobs[i].Reference)
		if !safeJobID.MatchString(jobs[i].ID) || jobs[i].ID == "." || jobs[i].ID == ".." {
			return nil, "", fmt.Errorf("第 %d 项 id 无效", i+1)
		}
		if jobs[i].Prompt == "" {
			return nil, "", fmt.Errorf("第 %d 项 prompt 为空", i+1)
		}
		if _, ok := seen[jobs[i].ID]; ok {
			return nil, "", fmt.Errorf("任务 id 重复: %s", jobs[i].ID)
		}
		seen[jobs[i].ID] = struct{}{}
	}
	absPlan, err := filepath.Abs(planPath)
	if err != nil {
		return nil, "", errors.New("无法解析绘图计划路径")
	}
	return jobs, filepath.Dir(absPlan), nil
}

func processJob(ctx context.Context, client *component.ReportIllustrationClient, cfg *config.Config, planDir, outputDir string, job illustrationJob) error {
	reference, referenceHash, err := loadReference(planDir, job.Reference)
	if err != nil {
		return err
	}
	inputHash := hashStrings(
		job.Prompt,
		component.ReportIllustrationProvider,
		cfg.AI.LaoZhangImageBaseURL,
		cfg.AI.LaoZhangImageModel,
		"2048x1152",
		referenceHash,
	)
	receiptPath := filepath.Join(outputDir, job.ID+".receipt.json")
	reused, err := reuseCompleted(receiptPath, outputDir, inputHash, cfg.AI.LaoZhangImageModel)
	if err != nil {
		return err
	}
	if reused {
		log.Printf("[%s] 复用已验证的 2K 图片", job.ID)
		return nil
	}

	submittedAt := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := illustrationReceipt{
		ID: job.ID, Status: "submitting", InputHash: inputHash, ReferenceHash: referenceHash,
		Provider: component.ReportIllustrationProvider, Model: cfg.AI.LaoZhangImageModel,
		Width: component.ReportIllustrationWidth, Height: component.ReportIllustrationHeight,
		SubmittedAt: submittedAt,
	}
	if err := writeReceiptExclusive(receiptPath, receipt); err != nil {
		return err
	}
	log.Printf("[%s] 已登记提交状态，开始生成 2K 图片", job.ID)
	result, err := client.Generate(ctx, component.ReportIllustrationRequest{Prompt: job.Prompt, ReferenceData: reference})
	if err != nil {
		// Keep submitting: the provider may have accepted a paid request even when
		// the client did not receive a usable response.
		return fmt.Errorf("服务结果不确定，已保留 submitting 收据，禁止自动重试: %w", err)
	}
	ext := ".png"
	if result.MIMEType == "image/jpeg" {
		ext = ".jpg"
	}
	imageName := job.ID + ext
	imagePath := filepath.Join(outputDir, imageName)
	if err := writeFileExclusive(imagePath, result.Data, 0o600); err != nil {
		return errors.New("图片文件已存在或无法保存；submitting 收据已保留")
	}
	receipt.Status = "complete"
	receipt.MIMEType = result.MIMEType
	receipt.ImageFile = imageName
	receipt.ImageSHA256 = hashBytes(result.Data)
	receipt.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := replaceReceipt(receiptPath, receipt); err != nil {
		return errors.New("图片已保存但完成收据写入失败；请人工核对，禁止自动重试")
	}
	log.Printf("[%s] 已生成 %dx%d %s", job.ID, result.Width, result.Height, result.MIMEType)
	return nil
}

func loadReference(planDir, reference string) ([]byte, string, error) {
	if reference == "" {
		return nil, "", nil
	}
	if filepath.IsAbs(reference) {
		return nil, "", errors.New("reference 必须是相对绘图计划的本地文件名")
	}
	root, err := filepath.EvalSymlinks(planDir)
	if err != nil {
		return nil, "", errors.New("无法解析绘图计划目录")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(planDir, filepath.Clean(reference)))
	if err != nil {
		return nil, "", errors.New("无法读取本地参考图")
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("reference 不得离开绘图计划目录")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, "", errors.New("无法读取本地参考图")
	}
	defer f.Close()
	data, err := readLimited(f, maxReferenceBytes)
	if err != nil {
		return nil, "", errors.New("本地参考图过大或无法读取")
	}
	return data, hashBytes(data), nil
}

func reuseCompleted(receiptPath, outputDir, inputHash, model string) (bool, error) {
	data, err := os.ReadFile(receiptPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("无法读取已有收据")
	}
	var receipt illustrationReceipt
	if json.Unmarshal(data, &receipt) != nil {
		return false, errors.New("已有收据损坏，拒绝覆盖")
	}
	if receipt.InputHash != inputHash || receipt.Model != model || receipt.Width != component.ReportIllustrationWidth || receipt.Height != component.ReportIllustrationHeight {
		return false, errors.New("已有收据与当前输入或模型不匹配，拒绝覆盖")
	}
	if receipt.Status == "submitting" {
		return false, errors.New("已有 submitting 收据，结果可能已计费，必须人工核对")
	}
	if receipt.Status != "complete" || receipt.ImageFile == "" {
		return false, errors.New("已有收据状态不可复用，拒绝覆盖")
	}
	imagePath := filepath.Join(outputDir, filepath.Base(receipt.ImageFile))
	imageData, err := os.ReadFile(imagePath)
	if err != nil || hashBytes(imageData) != receipt.ImageSHA256 {
		return false, errors.New("已有图片缺失或哈希不匹配，拒绝覆盖")
	}
	mimeType, width, height, err := component.ValidateReportIllustration(imageData)
	if err != nil || mimeType != receipt.MIMEType || width != receipt.Width || height != receipt.Height {
		return false, errors.New("已有图片未通过 2K 校验，拒绝覆盖")
	}
	return true, nil
}

func writeReceiptExclusive(path string, receipt illustrationReceipt) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return errors.New("无法编码提交收据")
	}
	data = append(data, '\n')
	if err := writeFileExclusive(path, data, 0o600); err != nil {
		return errors.New("已有收据或无法登记提交状态，拒绝发起付费请求")
	}
	return nil
}

func replaceReceipt(path string, receipt illustrationReceipt) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".receipt-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func writeFileExclusive(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("size limit exceeded")
	}
	return data, nil
}

func hashStrings(values ...string) string {
	h := sha256.New()
	for _, value := range values {
		_, _ = io.WriteString(h, value)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

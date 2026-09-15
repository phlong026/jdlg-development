package component

import (
	"strings"

	"github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dytnsapi "github.com/alibabacloud-go/dytnsapi-20200217/v4/client"
	"github.com/alibabacloud-go/tea/tea"

	"service/config"
)

// VerifyIDCardTwo 二要素核验（姓名 + 身份证号）
// 调用阿里云号码百科 CertNoTwoElementVerification 接口
// IsConsistent: 1=一致 0=不一致 2=查无
func VerifyIDCardTwo(realName, idCardNo string) (passed bool, msg string) {
	cfg := config.Get()
	if cfg.AliyunSMS.PersonalAuthCode == "" {
		return true, "" // 未配置授权码时放行，便于开发测试
	}
	realName = strings.TrimSpace(realName)
	idCardNo = strings.TrimSpace(idCardNo)
	if realName == "" || idCardNo == "" {
		return false, "姓名或身份证号不能为空"
	}

	openapiConfig := &client.Config{
		AccessKeyId:     tea.String(cfg.AliyunSMS.AccessKeyID),
		AccessKeySecret: tea.String(cfg.AliyunSMS.AccessKeySecret),
		Endpoint:        tea.String("dytnsapi.aliyuncs.com"),
	}
	c, err := dytnsapi.NewClient(openapiConfig)
	if err != nil {
		return false, "服务初始化失败"
	}

	req := &dytnsapi.CertNoTwoElementVerificationRequest{
		AuthCode: tea.String(cfg.AliyunSMS.PersonalAuthCode),
		CertName: tea.String(realName),
		CertNo:   tea.String(idCardNo),
	}

	resp, err := c.CertNoTwoElementVerification(req)
	if err != nil {
		return false, "核验请求失败"
	}

	if resp == nil || resp.Body == nil || resp.Body.Data == nil {
		return false, "核验返回异常"
	}

	code := tea.StringValue(resp.Body.Code)
	if code != "OK" {
		return false, "核验失败"
	}

	isConsistent := tea.StringValue(resp.Body.Data.IsConsistent)
	switch isConsistent {
	case "1":
		return true, ""
	case "0":
		return false, "姓名与身份证号不一致"
	case "2":
		return false, "查无此人"
	default:
		return false, "核验未通过"
	}
}

// VerifyEnterpriseThree 企业三要素核验（企业名称 + 统一社会信用代码 + 法人姓名）
// 调用阿里云号码百科 CompanyThreeElementsVerification 接口
func VerifyEnterpriseThree(companyName, creditCode, legalPerson string) (passed bool, msg string) {
	cfg := config.Get()
	if cfg.AliyunSMS.EnterpriseAuthCode == "" {
		return true, "" // 未配置授权码时放行
	}
	companyName = strings.TrimSpace(companyName)
	creditCode = strings.TrimSpace(creditCode)
	legalPerson = strings.TrimSpace(legalPerson)
	if companyName == "" || creditCode == "" || legalPerson == "" {
		return false, "企业名称、统一社会信用代码或法人姓名不能为空"
	}

	openapiConfig := &client.Config{
		AccessKeyId:     tea.String(cfg.AliyunSMS.AccessKeyID),
		AccessKeySecret: tea.String(cfg.AliyunSMS.AccessKeySecret),
		Endpoint:        tea.String("dytnsapi.aliyuncs.com"),
	}
	c, err := dytnsapi.NewClient(openapiConfig)
	if err != nil {
		return false, "服务初始化失败"
	}

	req := &dytnsapi.CompanyThreeElementsVerificationRequest{
		AuthCode:             tea.String(cfg.AliyunSMS.EnterpriseAuthCode),
		EpCertName:           tea.String(companyName),
		EpCertNo:             tea.String(creditCode),
		LegalPersonCertName:  tea.String(legalPerson),
	}

	resp, err := c.CompanyThreeElementsVerification(req)
	if err != nil {
		return false, "核验请求失败"
	}

	if resp == nil || resp.Body == nil || resp.Body.Data == nil {
		return false, "核验返回异常"
	}

	code := tea.StringValue(resp.Body.Code)
	if code != "OK" {
		return false, "核验失败"
	}

	verifyResult := tea.StringValue(resp.Body.Data.VerifyResult)
	reasonCode := tea.Int64Value(resp.Body.Data.ReasonCode)

	if strings.EqualFold(verifyResult, "true") && reasonCode == 0 {
		return true, ""
	}

	// ReasonCode: 0=一致 1=一致但企业非正常营业 2=人企不一致 3=企业二要素不通过 4=查无企业 5=人在库中不存在
	switch reasonCode {
	case 1:
		return false, "企业非正常营业"
	case 2:
		return false, "企业信息与法人不一致"
	case 3:
		return false, "企业名称或证件号有误"
	case 4:
		return false, "查无该企业"
	case 5:
		return false, "法人信息在库中不存在"
	default:
		return false, "核验未通过"
	}
}

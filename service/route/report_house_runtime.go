package route

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"service/component"
	"service/config"
	"service/report"
)

// Keep the business workflow independent of the mini-program provider package.
type houseIllustrationProvider struct {
	client *component.ReportIllustrationClient
}

func (p houseIllustrationProvider) Identity() report.HouseImageProviderIdentity {
	provider, model, version := p.client.Identity()
	return report.HouseImageProviderIdentity{Provider: provider, Model: model, ConfigVersion: version}
}

func (p houseIllustrationProvider) Generate(ctx context.Context, prompt string, reference []byte) (report.HouseGeneratedImage, error) {
	result, err := p.client.Generate(ctx, component.ReportIllustrationRequest{Prompt: prompt, ReferenceData: reference})
	if err != nil {
		return report.HouseGeneratedImage{}, err
	}
	return report.HouseGeneratedImage{Data: result.Data}, nil
}

func configuredHouseOrchestrator(ctx context.Context, db *sql.DB, cfg *config.Config, client report.AgentClient, checkpoints report.StageCheckpointStore, assetRoot string) (*report.HouseOrchestrator, error) {
	images, err := report.NewMySQLHouseImageStore(db)
	if err != nil {
		return nil, err
	}
	if err := images.VerifySchema(ctx); err != nil {
		return nil, err
	}
	provider, err := component.NewConfiguredReportIllustrationClient(ctx, db, cfg, &http.Client{
		Timeout:       180 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})
	if err != nil {
		return nil, err
	}
	service, err := report.NewHouseImageService(images, houseIllustrationProvider{client: provider}, assetRoot)
	if err != nil {
		return nil, err
	}
	return report.NewHouseOrchestrator(client, checkpoints, service)
}

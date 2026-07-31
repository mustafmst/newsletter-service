package worker

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/pmstowski/newsletter/internal/newsletter"
	"github.com/pmstowski/newsletter/internal/store"
)

func ScanOnce(ctx context.Context, st store.Store, dir string, defaultFromName string, logger *slog.Logger) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			logger.Warn("newsletter file skipped", "path", path, "error", err)
			return nil
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".html" {
			return nil
		}
		parsed, err := newsletter.ParseFile(path, defaultFromName)
		if err != nil {
			logger.Warn("newsletter parse failed", "path", path, "error", err)
			return nil
		}
		now := time.Now().UTC()
		campaign, created, err := st.CreateCampaignIfNew(ctx, store.CampaignInput{
			SourcePath:    parsed.Path,
			ContentSHA256: parsed.SHA256,
			Subject:       parsed.Subject,
			FromName:      parsed.FromName,
			HTML:          parsed.HTML,
		}, now)
		if err != nil {
			return err
		}
		if !created {
			logger.Info("newsletter campaign already processed", "path", path, "sha256", parsed.SHA256)
			return nil
		}
		count, err := st.CreateDeliveriesForCampaign(ctx, campaign.ID, now)
		if err != nil {
			return err
		}
		logger.Info("newsletter campaign created", "path", path, "sha256", parsed.SHA256, "deliveries", count)
		return nil
	})
}

func RunScanner(ctx context.Context, interval time.Duration, scan func(context.Context) error, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := scan(ctx); err != nil {
			logger.Error("newsletter scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

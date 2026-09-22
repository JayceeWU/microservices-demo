package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
)

func (s *Service) PrepareAttachment(ctx context.Context, actor ActorContext, conversationID string, kind domain.MessageKind, mime, sha string, size int64) (Attachment, string, time.Time, error) {
	if err := requireActor(actor); err != nil {
		return Attachment{}, "", time.Time{}, err
	}
	if !validAttachment(kind, mime, size, actor) {
		return Attachment{}, "", time.Time{}, appcore.Invalid("unsupported attachment type or size")
	}
	id := s.ids.New()
	acquired, err := s.bus.AcquireUpload(ctx, actor.UserID, id, 5, 15*time.Minute)
	if err != nil {
		return Attachment{}, "", time.Time{}, appcore.Unavailable("upload limiter unavailable", err)
	}
	if !acquired {
		return Attachment{}, "", time.Time{}, appcore.Conflict("no more than five uploads may run at once")
	}
	objectKey := fmt.Sprintf("quarantine/%s/%s", conversationID, id)
	attachment := Attachment{ID: id, ConversationID: conversationID, UploaderID: actor.UserID, Kind: kind, MimeType: mime, SizeBytes: size, SHA256: strings.ToLower(sha), ObjectKey: objectKey, Status: "PENDING_UPLOAD"}
	err = s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		allowed, _, accessErr := repos.Conversations().CanAccess(ctx, conversationID, actor.UserID)
		if accessErr != nil {
			return accessErr
		}
		if !allowed {
			return appcore.Denied("conversation membership required")
		}
		attachment, accessErr = repos.Attachments().Add(ctx, attachment)
		return accessErr
	})
	if err != nil {
		_ = s.bus.ReleaseUpload(ctx, actor.UserID, id)
		return Attachment{}, "", time.Time{}, err
	}
	expires := s.clock.Now().Add(15 * time.Minute)
	url, err := s.objects.PresignUpload(ctx, objectKey, mime, 15*time.Minute)
	if err != nil {
		_ = s.bus.ReleaseUpload(ctx, actor.UserID, id)
		_ = s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
			return repos.Attachments().DeletePending(ctx, id)
		})
	}
	return attachment, url, expires, err
}

func validAttachment(kind domain.MessageKind, mime string, size int64, actor ActorContext) bool {
	images := map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}
	if kind == domain.ImageMessage {
		return images[mime] && size > 0 && size <= 10*1024*1024
	}
	return kind == domain.VideoMessage && mime == "video/mp4" && size > 0 && size <= 500*1024*1024 && actor.HasTenantRole("teacher", "studio_admin")
}

func (s *Service) CompleteAttachment(ctx context.Context, actor ActorContext, id string) (Attachment, error) {
	var result Attachment
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var err error
		result, err = repos.Attachments().MarkPendingScan(ctx, id)
		return err
	})
	if err == nil {
		_ = s.bus.ReleaseUpload(ctx, result.UploaderID, result.ID)
		err = s.media.RequestScan(ctx, result)
	}
	return result, err
}

func (s *Service) AttachmentDownload(ctx context.Context, actor ActorContext, id string) (string, time.Time, error) {
	var attachment Attachment
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var err error
		attachment, err = repos.Attachments().Get(ctx, id, false)
		if err != nil {
			return err
		}
		if attachment.Status != "CLEAN" || attachment.CleanObjectKey == "" {
			return appcore.Conflict("attachment is not available")
		}
		allowed, _, err := repos.Conversations().CanAccess(ctx, attachment.ConversationID, actor.UserID)
		if err != nil || !allowed {
			return appcore.Denied("attachment access denied")
		}
		return nil
	})
	if err != nil {
		return "", time.Time{}, err
	}
	expires := s.clock.Now().Add(15 * time.Minute)
	url, err := s.objects.PresignDownload(ctx, attachment.CleanObjectKey, 15*time.Minute)
	return url, expires, err
}

func (s *Service) ReportMessage(ctx context.Context, actor ActorContext, messageID, reason string) (Report, error) {
	if strings.TrimSpace(reason) == "" {
		return Report{}, appcore.Invalid("reason is required")
	}
	var result Report
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var err error
		result, err = repos.Messages().Report(ctx, messageID, actor.UserID, strings.TrimSpace(reason))
		return err
	})
	return result, err
}

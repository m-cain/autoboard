package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const attachmentInlineLimit = int64(262_144)

type Attachment struct {
	ID               string      `json:"id" jsonschema_extras:"format=uuid"`
	TicketID         string      `json:"ticket_id" jsonschema_extras:"format=uuid"`
	ProjectID        string      `json:"project_id" jsonschema_extras:"format=uuid"`
	OriginalFilename string      `json:"original_filename" jsonschema_extras:"minLength=1"`
	MediaType        string      `json:"media_type" jsonschema_extras:"minLength=1"`
	ByteSize         int64       `json:"byte_size" jsonschema_extras:"minimum=0"`
	SHA256           string      `json:"sha256" jsonschema_extras:"pattern=^[a-f0-9]{64}$"`
	ManagedPath      string      `json:"-"`
	Attribution      Attribution `json:"attribution"`
	InsertedAt       time.Time   `json:"inserted_at"`
}

type AttachmentRead struct {
	Attachment
	Content     *string `json:"content,omitempty"`
	ManagedPath *string `json:"managed_path,omitempty"`
}

type stagedAttachment struct {
	path             string
	originalFilename string
	mediaType        string
	byteSize         int64
	sha256           string
}

func (s *Service) AddAttachmentFromPath(
	ctx context.Context,
	attribution Attribution,
	ticketID string,
	sourcePath string,
) (Attachment, Ticket, error) {
	if err := attribution.Validate(); err != nil {
		return Attachment{}, Ticket{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	attachmentID := uuid.NewString()
	staged, err := stageAttachment(
		sourcePath,
		filepath.Join(s.dataDir, "attachments", "tmp"),
		s.maxAttachmentBytes,
	)
	if err != nil {
		return Attachment{}, Ticket{}, err
	}
	defer os.Remove(staged.path)

	finalDir := filepath.Join(s.dataDir, "attachments")
	finalPath := filepath.Join(finalDir, attachmentID)
	if err := os.Rename(staged.path, finalPath); err != nil {
		return Attachment{}, Ticket{}, validationError(
			"source_path",
			"could not be copied",
		)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(finalPath)
		}
	}()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attachment{}, Ticket{}, fmt.Errorf(
			"begin attachment add: %w",
			err,
		)
	}
	defer tx.Rollback()
	ticket, err := loadTicket(ctx, tx, ticketID)
	if err != nil {
		return Attachment{}, Ticket{}, err
	}
	if err := requireActiveTicketProject(ctx, tx, ticket.ProjectID); err != nil {
		return Attachment{}, Ticket{}, err
	}
	now := time.Now().UTC()
	attachment := Attachment{
		ID:               attachmentID,
		TicketID:         ticket.ID,
		ProjectID:        ticket.ProjectID,
		OriginalFilename: staged.originalFilename,
		MediaType:        staged.mediaType,
		ByteSize:         staged.byteSize,
		SHA256:           staged.sha256,
		ManagedPath:      finalPath,
		Attribution:      attribution,
		InsertedAt:       now,
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO attachments
		 (id, ticket_id, project_id, original_filename, media_type, byte_size,
		  sha256, managed_path, performed_by, initiated_by, inserted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attachment.ID,
		attachment.TicketID,
		attachment.ProjectID,
		attachment.OriginalFilename,
		attachment.MediaType,
		attachment.ByteSize,
		attachment.SHA256,
		attachment.ManagedPath,
		attachment.Attribution.PerformedBy,
		attachment.Attribution.InitiatedBy,
		formatTime(attachment.InsertedAt),
	); err != nil {
		return Attachment{}, Ticket{}, fmt.Errorf("insert attachment: %w", err)
	}
	ticket.Revision++
	ticket.UpdatedAt = now
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tickets SET revision = ?, updated_at = ? WHERE id = ?`,
		ticket.Revision,
		formatTime(now),
		ticket.ID,
	); err != nil {
		return Attachment{}, Ticket{}, fmt.Errorf(
			"advance attachment ticket revision: %w",
			err,
		)
	}
	if err := insertActivity(
		ctx,
		tx,
		attribution,
		"attachment.added",
		ticket.ProjectID,
		&ticket.ID,
		map[string]any{
			"attachment_id":     attachment.ID,
			"original_filename": attachment.OriginalFilename,
			"media_type":        attachment.MediaType,
			"byte_size":         attachment.ByteSize,
			"sha256":            attachment.SHA256,
		},
		now,
	); err != nil {
		return Attachment{}, Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return Attachment{}, Ticket{}, fmt.Errorf("commit attachment add: %w", err)
	}
	committed = true
	return attachment, ticket, nil
}

func (s *Service) ReadAttachment(
	ctx context.Context,
	attachmentID string,
) (AttachmentRead, error) {
	attachment, err := loadAttachment(ctx, s.db, attachmentID)
	if err != nil {
		return AttachmentRead{}, err
	}
	file, err := os.Open(attachment.ManagedPath)
	if err != nil {
		return AttachmentRead{}, domainError(
			ErrorAttachmentFailed,
			"managed attachment could not be read",
		)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return AttachmentRead{}, domainError(
			ErrorAttachmentFailed,
			"managed attachment could not be read",
		)
	}
	read := AttachmentRead{Attachment: attachment}
	if !inlineMediaType(attachment.MediaType) ||
		attachment.ByteSize > attachmentInlineLimit {
		read.ManagedPath = &read.Attachment.ManagedPath
		return read, nil
	}
	content, err := io.ReadAll(io.LimitReader(file, attachmentInlineLimit+1))
	if err != nil {
		return AttachmentRead{}, domainError(
			ErrorAttachmentFailed,
			"managed attachment could not be read",
		)
	}
	if int64(len(content)) <= attachmentInlineLimit && utf8.Valid(content) {
		value := string(content)
		read.Content = &value
		return read, nil
	}
	read.ManagedPath = &read.Attachment.ManagedPath
	return read, nil
}

func (s *Service) GetAttachment(
	ctx context.Context,
	attachmentID string,
) (Attachment, error) {
	return loadAttachment(ctx, s.db, attachmentID)
}

func loadAttachment(
	ctx context.Context,
	query queryRower,
	attachmentID string,
) (Attachment, error) {
	var attachment Attachment
	var insertedAt string
	err := query.QueryRowContext(
		ctx,
		`SELECT id, ticket_id, project_id, original_filename, media_type,
		        byte_size, sha256, managed_path, performed_by, initiated_by, inserted_at
		 FROM attachments
		 WHERE id = ?`,
		attachmentID,
	).Scan(
		&attachment.ID,
		&attachment.TicketID,
		&attachment.ProjectID,
		&attachment.OriginalFilename,
		&attachment.MediaType,
		&attachment.ByteSize,
		&attachment.SHA256,
		&attachment.ManagedPath,
		&attachment.Attribution.PerformedBy,
		&attachment.Attribution.InitiatedBy,
		&insertedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Attachment{}, domainError(ErrorNotFound, "attachment not found")
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("load attachment: %w", err)
	}
	attachment.InsertedAt, err = time.Parse(time.RFC3339Nano, insertedAt)
	if err != nil {
		return Attachment{}, fmt.Errorf(
			"parse attachment timestamp: %w",
			err,
		)
	}
	return attachment, nil
}

func stageAttachment(
	sourcePath string,
	tempDir string,
	maxBytes int64,
) (stagedAttachment, error) {
	if !filepath.IsAbs(sourcePath) {
		return stagedAttachment{}, validationError(
			"source_path",
			"must be an absolute path",
		)
	}
	before, err := os.Lstat(sourcePath)
	if err != nil || !before.Mode().IsRegular() {
		return stagedAttachment{}, validationError(
			"source_path",
			"must be a regular non-symlink file",
		)
	}
	if before.Size() > maxBytes {
		return stagedAttachment{}, validationError(
			"source_path",
			"exceeds the configured attachment size limit",
		)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return stagedAttachment{}, validationError(
			"source_path",
			"could not be read",
		)
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return stagedAttachment{}, validationError(
			"source_path",
			"changed while being copied",
		)
	}
	if err := ensurePrivateDirectory(filepath.Dir(tempDir)); err != nil {
		return stagedAttachment{}, validationError(
			"source_path",
			"could not be copied",
		)
	}
	if err := ensurePrivateDirectory(tempDir); err != nil {
		return stagedAttachment{}, validationError(
			"source_path",
			"could not be copied",
		)
	}
	target, err := os.CreateTemp(tempDir, "staged-*")
	if err != nil {
		return stagedAttachment{}, validationError(
			"source_path",
			"could not be copied",
		)
	}
	targetPath := target.Name()
	keep := false
	defer func() {
		_ = target.Close()
		if !keep {
			_ = os.Remove(targetPath)
		}
	}()
	if err := target.Chmod(0o600); err != nil {
		return stagedAttachment{}, validationError(
			"source_path",
			"could not be copied",
		)
	}
	hash := sha256.New()
	written, err := io.Copy(
		io.MultiWriter(target, hash),
		io.LimitReader(source, maxBytes+1),
	)
	if err != nil {
		return stagedAttachment{}, validationError(
			"source_path",
			"could not be read",
		)
	}
	if written > maxBytes {
		return stagedAttachment{}, validationError(
			"source_path",
			"exceeds the configured attachment size limit",
		)
	}
	after, err := source.Stat()
	if err != nil ||
		!os.SameFile(before, after) ||
		after.Size() != before.Size() ||
		written != before.Size() ||
		after.ModTime() != before.ModTime() {
		return stagedAttachment{}, validationError(
			"source_path",
			"changed while being copied",
		)
	}
	if err := target.Sync(); err != nil {
		return stagedAttachment{}, validationError(
			"source_path",
			"could not be copied",
		)
	}
	if err := target.Close(); err != nil {
		return stagedAttachment{}, validationError(
			"source_path",
			"could not be copied",
		)
	}
	keep = true
	return stagedAttachment{
		path:             targetPath,
		originalFilename: filepath.Base(sourcePath),
		mediaType:        attachmentMediaType(sourcePath),
		byteSize:         written,
		sha256:           hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func attachmentMediaType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".csv":
		return "text/csv"
	case ".html":
		return "text/html"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

func inlineMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "text/") ||
		mediaType == "application/json" ||
		mediaType == "application/xml"
}

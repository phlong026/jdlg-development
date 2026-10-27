package tracking

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// MediaObjects is server-owned storage. Access checks stay in Media/ReadContent;
// implementations return bytes, never a public URL that bypasses those checks.
type MediaObjectStore interface {
	Put(context.Context, string, string, []byte, string) error
	Get(context.Context, string, string, int64) ([]byte, string, error)
}

const (
	mediaPendingKind = "media_pending"
	mediaObjectKind  = "media_object"
)

type mediaObjectJournal struct {
	Owner     string    `json:"owner_id"`
	Asset     Asset     `json:"asset"`
	SHA256    string    `json:"sha256"`
	Origin    string    `json:"origin"`
	Operation string    `json:"operation"`
	CreatedAt time.Time `json:"created_at"`
}

// PrepareMediaObject records intent before an external object-store write.
// The operation token is stable across retries and process restarts.
func (s *Service) PrepareMediaObject(ctx context.Context, scope Scope, asset Asset, owner, sha256, origin string) (string, bool, error) {
	if err := s.validateMediaWrite(scope, asset, owner); err != nil {
		return "", false, err
	}
	sha256 = strings.ToLower(sha256)
	if !validSHA256(sha256) || !validMediaOrigin(origin) {
		return "", false, fail(400, "媒体对象校验信息无效")
	}

	var operation string
	already := false
	err := s.transaction(ctx, func(r Repository) error {
		proof, proofErr := decode[mediaObjectJournal](r, mediaObjectKind, asset.ID)
		if proofErr == nil {
			stored, mediaErr := decode[Asset](r, "media", asset.ID)
			if mediaErr != nil {
				return ErrUnavailable
			}
			if !scope.Allows(proof.Owner) || !sameMediaJournal(proof, owner, asset, sha256, origin) || stored != asset {
				return fail(409, "媒体对象标识冲突")
			}
			operation, already = proof.Operation, true
			return nil
		}
		if !errors.Is(proofErr, ErrNotFound) {
			return proofErr
		}
		if _, mediaErr := r.Get("media", asset.ID); mediaErr == nil {
			// A legacy row has no digest proof, so it cannot prove which remote
			// bytes a retry would reuse.
			return fail(409, "媒体对象缺少校验记录")
		} else if !errors.Is(mediaErr, ErrNotFound) {
			return mediaErr
		}

		pending, pendingErr := decode[mediaObjectJournal](r, mediaPendingKind, asset.ID)
		if pendingErr == nil {
			if !scope.Allows(pending.Owner) || !sameMediaJournal(pending, owner, asset, sha256, origin) {
				return fail(409, "媒体对象标识冲突")
			}
			operation = pending.Operation
			return nil
		}
		if !errors.Is(pendingErr, ErrNotFound) {
			return pendingErr
		}

		operation = opaque()
		pending = mediaObjectJournal{Owner: owner, Asset: asset, SHA256: sha256, Origin: origin, Operation: operation, CreatedAt: s.Now()}
		return r.Insert(record(mediaPendingKind, asset.ID, owner, s.Now(), pending))
	})
	return operation, already, err
}

// FinalizeMediaObject makes media metadata visible only after the caller has
// completed the external object-store write. A private digest record lets a
// retry distinguish a committed transaction from a rolled-back one.
func (s *Service) FinalizeMediaObject(ctx context.Context, scope Scope, id, operation string) (Asset, error) {
	var asset Asset
	if err := s.authorized(scope); err != nil {
		return asset, err
	}
	if !scope.Manage || !validID(id) || !validID(operation) {
		return asset, fail(403, "媒体对象完成凭据无效")
	}

	var expected mediaObjectJournal
	err := s.transaction(ctx, func(r Repository) error {
		proof, proofErr := decode[mediaObjectJournal](r, mediaObjectKind, id)
		if proofErr == nil {
			if proof.Operation != operation || !scope.Allows(proof.Owner) {
				return fail(409, "媒体对象完成凭据冲突")
			}
			stored, mediaErr := decode[Asset](r, "media", id)
			if mediaErr != nil || stored != proof.Asset {
				return ErrUnavailable
			}
			expected, asset = proof, stored
			return nil
		}
		if !errors.Is(proofErr, ErrNotFound) {
			return proofErr
		}

		pending, pendingErr := decode[mediaObjectJournal](r, mediaPendingKind, id)
		if pendingErr != nil {
			if errors.Is(pendingErr, ErrNotFound) {
				return fail(409, "媒体对象待完成记录不存在")
			}
			return pendingErr
		}
		if pending.Operation != operation || pending.Asset.ID != id || !scope.Allows(pending.Owner) {
			return fail(409, "媒体对象完成凭据冲突")
		}
		expected, asset = pending, pending.Asset
		now := s.Now()
		if err := s.addMediaIn(r, scope, pending.Asset, pending.Owner, now); err != nil {
			return err
		}
		if err := r.Insert(record(mediaObjectKind, id, pending.Owner, now, pending)); err != nil {
			return err
		}
		return r.Remove(mediaPendingKind, id)
	})
	if err == nil {
		return asset, nil
	}
	if expected.Operation != operation {
		return Asset{}, err
	}
	if recovered, ok := s.recoverFinalizedMediaObject(ctx, scope, expected); ok {
		return recovered, nil
	}
	return Asset{}, err
}

func (s *Service) recoverFinalizedMediaObject(ctx context.Context, scope Scope, expected mediaObjectJournal) (Asset, bool) {
	var asset Asset
	err := s.readTransaction(ctx, func(r Repository) error {
		proof, err := decode[mediaObjectJournal](r, mediaObjectKind, expected.Asset.ID)
		if err != nil || proof != expected || !scope.Allows(proof.Owner) {
			return ErrUnavailable
		}
		asset, err = decode[Asset](r, "media", expected.Asset.ID)
		if err != nil || asset != expected.Asset {
			return ErrUnavailable
		}
		return nil
	})
	return asset, err == nil
}

func sameMediaJournal(journal mediaObjectJournal, owner string, asset Asset, sha256, origin string) bool {
	return journal.Owner == owner && journal.Asset == asset && journal.SHA256 == sha256 && journal.Origin == origin
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validMediaOrigin(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == ':' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

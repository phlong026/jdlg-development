package report

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

const houseStorageMarkerName = ".house-image-storage-id"

type houseImageStorageVerifier interface {
	VerifyStorage(context.Context, string) error
}

// loadOrCreateHouseStorageID identifies the physical private asset store, not
// its path. All workers connected to one database must see this same marker on
// shared durable storage. Copying the marker to a different disk defeats the
// fencing guarantee and is unsupported.
func loadOrCreateHouseStorageID(root string) (string, error) {
	marker := filepath.Join(root, houseStorageMarkerName)
	if info, err := os.Lstat(marker); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return "", ErrInvalidInput
		}
		return readHouseStorageID(marker)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", imageIOFailure("stat_storage_identity", "", err)
	}

	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", imageIOFailure("create_storage_identity", "", err)
	}
	candidate := hex.EncodeToString(random[:])
	// writePrivateImageFile publishes with an atomic hard link. Concurrent
	// starters either publish one complete marker or read that winner.
	if err := writePrivateImageFile(marker, []byte(candidate)); err != nil && !errors.Is(err, os.ErrExist) {
		return "", imageIOFailure("save_storage_identity", "", err)
	}
	return readHouseStorageID(marker)
}

func readHouseStorageID(marker string) (string, error) {
	raw, err := readPrivateImageFile(marker, 64)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			return "", ErrInvalidInput
		}
		return "", imageIOFailure("read_storage_identity", "", err)
	}
	id := string(raw)
	if !isImageHash(id) {
		return "", ErrInvalidInput
	}
	return id, nil
}

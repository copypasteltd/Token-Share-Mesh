package node

import (
	"bytes"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
)

const MaximumRentalPreparations = 8

type rentalPreparationRecord struct {
	Approval rentalaccess.LocalApproval `json:"local_approval"`
	Manifest journalBytes               `json:"original_manifest"`
}

// RecordPreparation accepts only an explicit local approval supplied by the
// trusted owner configuration/UI path. No remote handler auto-creates it.
// Original approval/manifest are encrypted and flushed before reserving SQL.
func (j *RentalJournal) RecordPreparation(p rentalaccess.Prepared, now time.Time) error {
	if j == nil {
		return ErrRentalJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.closing || j.poisoned {
		return ErrRentalJournal
	}
	m := p.Details()
	if m.Device != j.node.s.DeviceID || m.Seller != j.node.s.SellerID || p.Digest() == "" {
		return ErrRentalJournal
	}
	for _, r := range j.s.Preparations {
		if r.Approval.Manifest.Session == m.Session {
			original, err := rentalaccess.DecodePreparationRecord(r.Manifest, r.Approval)
			if err != nil || original.ApprovalDigest() != p.ApprovalDigest() || !bytes.Equal(r.Manifest, p.Raw()) {
				return ErrRentalJournal
			}
			return nil
		}
	}
	if !p.ValidAt(now) || len(j.s.Preparations) >= MaximumRentalPreparations {
		return ErrRentalJournal
	}
	j.s.Preparations = append(j.s.Preparations, rentalPreparationRecord{Approval: p.Approval(), Manifest: p.Raw()})
	if j.persistLocked() != nil {
		j.poisoned = true
		return ErrRentalJournal
	}
	return nil
}

func (j *RentalJournal) matchesPreparation(p rentalaccess.Prepared) bool {
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.closing || j.poisoned {
		return false
	}
	for _, r := range j.s.Preparations {
		if r.Approval.Manifest.Session == p.Details().Session {
			old, err := rentalaccess.DecodePreparationRecord(r.Manifest, r.Approval)
			return err == nil && old.ApprovalDigest() == p.ApprovalDigest() && bytes.Equal(r.Manifest, p.Raw())
		}
	}
	return false
}

// ApprovedPreparation recovers metadata, not permission to resume an owner.
// A past preparation TTL stays past, and the shared gate quarantines on reopen.
func (j *RentalJournal) ApprovedPreparation(session string) (rentalaccess.Prepared, error) {
	if j == nil {
		return rentalaccess.Prepared{}, ErrRentalJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.closing || j.poisoned {
		return rentalaccess.Prepared{}, ErrRentalJournal
	}
	for _, r := range j.s.Preparations {
		if r.Approval.Manifest.Session == session {
			return rentalaccess.DecodePreparationRecord(r.Manifest, r.Approval)
		}
	}
	return rentalaccess.Prepared{}, ErrRentalJournal
}

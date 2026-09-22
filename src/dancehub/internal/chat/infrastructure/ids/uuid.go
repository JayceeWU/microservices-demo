package ids

import "github.com/google/uuid"

type UUID struct{}

func (UUID) New() string { return uuid.NewString() }

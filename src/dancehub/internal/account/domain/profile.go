package domain

import (
	"fmt"
	"strings"
)

// Profile is the mutable public and teacher-facing information for a user.
// Email is set at provisioning time and is not mutable through this
// aggregate.
type Profile struct {
	userID       string
	email        string
	displayName  string
	avatarURL    string
	timezone     string
	bio          string
	portfolioURL string
}

type ProfileSnapshot struct {
	UserID, Email, DisplayName, AvatarURL, Timezone, Bio, PortfolioURL string
}

func RehydrateProfile(v ProfileSnapshot) *Profile {
	return &Profile{
		userID: v.UserID, email: v.Email, displayName: v.DisplayName,
		avatarURL: v.AvatarURL, timezone: v.Timezone, bio: v.Bio, portfolioURL: v.PortfolioURL,
	}
}

func (p *Profile) Snapshot() ProfileSnapshot {
	return ProfileSnapshot{
		UserID: p.userID, Email: p.email, DisplayName: p.displayName,
		AvatarURL: p.avatarURL, Timezone: p.timezone, Bio: p.bio, PortfolioURL: p.portfolioURL,
	}
}

// Update applies self-service profile edits. DisplayName and Timezone are
// required; the remaining fields are optional free text/URLs.
func (p *Profile) Update(displayName, avatarURL, timezone, bio, portfolioURL string) error {
	displayName, timezone = strings.TrimSpace(displayName), strings.TrimSpace(timezone)
	if displayName == "" || timezone == "" {
		return fmt.Errorf("%w: display_name and timezone are required", ErrInvalidArgument)
	}
	p.displayName, p.avatarURL, p.timezone, p.bio, p.portfolioURL = displayName, avatarURL, timezone, bio, portfolioURL
	return nil
}

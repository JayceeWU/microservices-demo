package postgres

import (
	"context"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5"
)

func NewRepositories(tx pgx.Tx) application.Repositories {
	return repositories{tx: tx}
}

type repositories struct{ tx pgx.Tx }

func (r repositories) Studios() application.StudioRepository     { return studioRepository{tx: r.tx} }
func (r repositories) Rooms() application.RoomRepository         { return roomRepository{tx: r.tx} }
func (r repositories) Products() application.ProductRepository   { return productRepository{tx: r.tx} }
func (r repositories) Campaigns() application.CampaignRepository { return campaignRepository{tx: r.tx} }

type scanner interface{ Scan(...any) error }

const studioColumns = `id,slug,name,description,address_line,city,state,postal_code,timezone,COALESCE(image_url,'')`

func scanStudio(row scanner) (domain.Studio, error) {
	var value domain.Studio
	err := row.Scan(&value.ID, &value.Slug, &value.Name, &value.Description, &value.AddressLine, &value.City, &value.State, &value.PostalCode, &value.Timezone, &value.ImageURL)
	return value, err
}

type studioRepository struct{ tx pgx.Tx }

func (r studioRepository) Search(ctx context.Context, query string, limit int32) ([]domain.Studio, error) {
	rows, err := r.tx.Query(ctx, `SELECT `+studioColumns+` FROM catalog.studios WHERE active AND (name ILIKE $1 OR city ILIKE $1) ORDER BY name LIMIT $2`, query, limit)
	if err != nil {
		return nil, translate(err, "unable to load studios")
	}
	defer rows.Close()
	result := make([]domain.Studio, 0)
	for rows.Next() {
		item, err := scanStudio(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, translate(rows.Err(), "unable to load studios")
}

func (r studioRepository) Get(ctx context.Context, id string) (domain.Studio, error) {
	item, err := scanStudio(r.tx.QueryRow(ctx, `SELECT `+studioColumns+` FROM catalog.studios WHERE id=$1 AND active`, id))
	if err != nil {
		return domain.Studio{}, translate(err, "studio not found")
	}
	return item, nil
}

func (r studioRepository) BatchGet(ctx context.Context, ids []string) ([]domain.Studio, error) {
	rows, err := r.tx.Query(ctx, `SELECT `+studioColumns+` FROM catalog.studios WHERE id=ANY($1::uuid[]) ORDER BY name`, ids)
	if err != nil {
		return nil, translate(err, "unable to load studios")
	}
	defer rows.Close()
	result := make([]domain.Studio, 0)
	for rows.Next() {
		item, err := scanStudio(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, translate(rows.Err(), "unable to load studios")
}

const roomColumns = `id,studio_id,name,capacity,rental_rate_cents_per_hour,facilities,COALESCE(image_url,'')`

func scanRoom(row scanner) (domain.Room, error) {
	var value domain.Room
	err := row.Scan(&value.ID, &value.StudioID, &value.Name, &value.Capacity, &value.RentalRateCentsPerHour, &value.Facilities, &value.ImageURL)
	return value, err
}

type roomRepository struct{ tx pgx.Tx }

func (r roomRepository) ListByStudio(ctx context.Context, studioID string, limit int32) ([]domain.Room, error) {
	rows, err := r.tx.Query(ctx, `SELECT `+roomColumns+` FROM catalog.rooms WHERE studio_id=$1 AND rentable ORDER BY name LIMIT $2`, studioID, limit)
	if err != nil {
		return nil, translate(err, "unable to load rooms")
	}
	defer rows.Close()
	result := make([]domain.Room, 0)
	for rows.Next() {
		item, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, translate(rows.Err(), "unable to load rooms")
}

func (r roomRepository) Get(ctx context.Context, id, studioID string) (domain.Room, error) {
	item, err := scanRoom(r.tx.QueryRow(ctx, `SELECT `+roomColumns+` FROM catalog.rooms WHERE id=$1 AND ($2='' OR studio_id::text=$2)`, id, studioID))
	if err != nil {
		return domain.Room{}, translate(err, "room not found")
	}
	return item, nil
}

const productColumns = `pv.id,p.id,COALESCE(p.studio_id::text,''),p.issuer_scope::text,p.kind::text,p.name,pv.amount_cents,COALESCE(pv.credit_amount,0),COALESCE(pv.validity_days,0),pv.final_sale`
const productFrom = `FROM catalog.credit_products p JOIN catalog.credit_product_versions pv ON pv.product_id=p.id`

func scanProduct(row scanner) (domain.CreditProductVersion, error) {
	var value domain.CreditProductVersion
	var scope, kind string
	if err := row.Scan(&value.ID, &value.ProductID, &value.StudioID, &scope, &kind, &value.Name, &value.AmountCents, &value.CreditAmount, &value.ValidityDays, &value.FinalSale); err != nil {
		return domain.CreditProductVersion{}, err
	}
	if scope == "PLATFORM" {
		value.IssuerScope = domain.IssuerScopePlatform
	} else {
		value.IssuerScope = domain.IssuerScopeStudio
	}
	if kind == "UNLIMITED" {
		value.Kind = domain.ProductKindUnlimited
	} else {
		value.Kind = domain.ProductKindCredits
	}
	return value, nil
}

type productRepository struct{ tx pgx.Tx }

func (r productRepository) Search(ctx context.Context, studioID string, includePlatform bool, limit int32) ([]domain.CreditProductVersion, error) {
	query := `SELECT ` + productColumns + ` ` + productFrom + ` WHERE p.active AND pv.valid_from<=now() AND (pv.valid_until IS NULL OR pv.valid_until>now()) AND (($2 AND p.issuer_scope='PLATFORM') OR p.studio_id::text=$1) ORDER BY p.issuer_scope DESC,p.name LIMIT $3`
	rows, err := r.tx.Query(ctx, query, studioID, includePlatform, limit)
	if err != nil {
		return nil, translate(err, "unable to load products")
	}
	defer rows.Close()
	result := make([]domain.CreditProductVersion, 0)
	for rows.Next() {
		item, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, translate(rows.Err(), "unable to load products")
}

func (r productRepository) Get(ctx context.Context, id string) (domain.CreditProductVersion, error) {
	query := `SELECT ` + productColumns + ` ` + productFrom + ` WHERE pv.id=$1`
	item, err := scanProduct(r.tx.QueryRow(ctx, query, id))
	if err != nil {
		return domain.CreditProductVersion{}, translate(err, "product version not found")
	}
	return item, nil
}

type campaignRepository struct{ tx pgx.Tx }

func (r campaignRepository) Get(ctx context.Context, id string) (domain.CampaignSnapshot, error) {
	var value domain.CampaignSnapshot
	err := r.tx.QueryRow(ctx, `SELECT id,COALESCE(studio_id::text,''),product_version_id,inventory,per_user_limit,payment_ttl_minutes,name,starts_at,ends_at,active FROM catalog.campaigns WHERE id=$1`, id).
		Scan(&value.ID, &value.StudioID, &value.ProductVersionID, &value.Inventory, &value.PerUserLimit, &value.PaymentTTLMinutes, &value.Name, &value.StartsAt, &value.EndsAt, &value.Active)
	if err != nil {
		return domain.CampaignSnapshot{}, translate(err, "campaign not found")
	}
	return value, nil
}

func translate(err error, message string) error {
	return platform.TranslateDatabaseError(err, message, application.NotFound, application.Conflict)
}

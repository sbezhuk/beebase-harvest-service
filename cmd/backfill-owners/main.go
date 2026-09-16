package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/sbezhuk/beebase-harvest-service/internal/platform/postgres"
	repo "github.com/sbezhuk/beebase-harvest-service/internal/repository/postgres"
)

type ownerResponse struct {
	UserID uuid.UUID `json:"userId"`
}

func main() {
	_ = godotenv.Load()
	ctx := context.Background()
	db, err := postgres.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	r := repo.NewHarvestRepository(db)
	base := os.Getenv("HIVE_SERVICE_URL")
	token := os.Getenv("INTERNAL_SERVICE_TOKEN")
	client := &http.Client{Timeout: 10 * time.Second}
	var scanned, resolved, unresolved, failed int
	var after uuid.UUID
	for {
		rows, err := r.ListLegacy(ctx, after, 100)
		if err != nil {
			panic(err)
		}
		if len(rows) == 0 {
			fmt.Printf("harvest owner backfill: scanned=%d already_owned=0 resolved=%d unresolved=%d failed=%d\n", scanned, resolved, unresolved, failed)
			return
		}
		for _, row := range rows {
			scanned++
			after = row.ID
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/internal/api/v1/hives/"+row.HiveID.String()+"/owner", nil)
			if err != nil {
				failed++
				_ = r.RecordBackfillFailure(ctx, row.ID, row.HiveID, "invalid hive owner request")
				continue
			}
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := client.Do(req)
			if err != nil {
				failed++
				_ = r.RecordBackfillFailure(ctx, row.ID, row.HiveID, "hive service unavailable")
				continue
			}
			var owner ownerResponse
			err = json.NewDecoder(resp.Body).Decode(&owner)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || err != nil || owner.UserID == uuid.Nil {
				unresolved++
				_ = r.RecordBackfillFailure(ctx, row.ID, row.HiveID, "hive owner unresolved")
				continue
			}
			if err := r.SetUserID(ctx, row.ID, owner.UserID); err != nil {
				panic(err)
			}
			resolved++
		}
		time.Sleep(10 * time.Millisecond)
	}
}

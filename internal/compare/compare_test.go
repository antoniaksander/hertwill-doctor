package compare

import (
	"testing"

	"github.com/antoniaksander/hertwill-doctor/internal/model"
)

func TestProductsCompareRows(t *testing.T) {
	wcImages := 4
	hwImages := 6
	rows := Products(
		model.Product{Found: true, Name: "Pilot Boots", SKU: "ABC123", Price: "59.00", ImageCount: &wcImages, Status: "publish"},
		model.Product{Found: true, Name: "Pilot Boots", SKU: "ABC123", Price: "59.00", ImageCount: &hwImages, Status: "synced", Variations: []model.Variation{{ID: "v1"}}},
	)
	if len(rows) == 0 {
		t.Fatal("expected rows")
	}
	var foundImages, foundVariations bool
	for _, row := range rows {
		if row.Field == "Images" {
			foundImages = true
			if row.Match != "no" {
				t.Fatalf("image match = %q", row.Match)
			}
		}
		if row.Field == "Variations" {
			foundVariations = true
			if row.Hertwill != "1" {
				t.Fatalf("variation row = %+v", row)
			}
		}
	}
	if !foundImages {
		t.Fatal("expected Images row")
	}
	if !foundVariations {
		t.Fatal("expected Variations row")
	}
}

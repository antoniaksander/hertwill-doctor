package model

type Product struct {
	Found      bool        `json:"found"`
	ID         string      `json:"id,omitempty"`
	Name       string      `json:"name,omitempty"`
	SKU        string      `json:"sku,omitempty"`
	Price      string      `json:"price,omitempty"`
	Stock      string      `json:"stock,omitempty"`
	ImageCount *int        `json:"image_count,omitempty"`
	ImageURLs  []string    `json:"image_urls,omitempty"`
	Status     string      `json:"status,omitempty"`
	Brand      string      `json:"brand,omitempty"`
	Category   string      `json:"category,omitempty"`
	Categories []string    `json:"categories,omitempty"`
	Slug       string      `json:"slug,omitempty"`
	Variations []Variation `json:"variations,omitempty"`
}

type Variation struct {
	ID         string            `json:"id,omitempty"`
	SKU        string            `json:"sku,omitempty"`
	Name       string            `json:"name,omitempty"`
	Price      string            `json:"price,omitempty"`
	Stock      string            `json:"stock,omitempty"`
	ImageCount *int              `json:"image_count,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

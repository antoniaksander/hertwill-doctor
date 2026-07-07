package hertwill

const (
	DefaultBaseURL = "https://api.hertwill.com"

	ListProductsPath  = "/v1/products"
	ProductSearchPath = "/v1/products/search"
	ProductPath       = "/v1/products/{id}"             // needs verification
	SyncStatusPath    = "/v1/products/{id}/sync-status" // needs verification
	LoginPath         = "/v1/auth/login"
	RegisterPath      = "/v1/auth/register"
	APIKeysPath       = "/v1/api-keys"
	ImportListPath    = "/v1/import-list/products"
	SyncProductsPath  = "/v1/sync/products"
)

func ProductSearchEndpointVerified() bool {
	return true
}

func LightweightHealthEndpointVerified() bool {
	return false
}

func EmailPasswordAuthVerified() bool {
	return false
}

func SyncStatusEndpointVerified() bool {
	return false
}

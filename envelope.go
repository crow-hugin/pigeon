package pigeon

// 信封
type envelope struct {
	t       int
	message []byte
	filter  filterFunc
}

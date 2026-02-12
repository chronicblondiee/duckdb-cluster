package router

func MergeResults(results [][]map[string]any) []map[string]any {
	var merged []map[string]any
	for _, r := range results {
		merged = append(merged, r...)
	}
	return merged
}

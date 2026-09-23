package source

import "time"

// ApplyMeta adds non-empty metadata fields from meta to the event log fields
// map. It is used when building "add" and "q" event rows.
func ApplyMeta(fields map[string]any, meta Meta) {
	if meta.Commit != "" {
		fields["commit"] = meta.Commit
	}
	if meta.URL != "" {
		fields["url"] = meta.URL
	}
	if meta.MIME != "" {
		fields["mime"] = meta.MIME
	}
	if meta.Converter != "" {
		fields["converter"] = meta.Converter
	}
	if meta.ConverterVersion != "" {
		fields["converter_version"] = meta.ConverterVersion
	}
	if !meta.FetchedAt.IsZero() {
		fields["fetched_at"] = meta.FetchedAt.UTC().Format(time.RFC3339)
	}
}

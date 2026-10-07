package tag

import "sort"

// Keys lists portable and commonly used freeform text keys, not a whitelist.
func Keys() []string {
	known := map[string]bool{}
	for key := range id3URLs {
		known[key] = true
	}
	for key := range id3v1Fields {
		known["id3v1."+key] = true
	}
	known["id3v1.tracknumber"] = true
	known["id3v1.genre"] = true
	for key := range bextStrings {
		known["bext."+key] = true
	}
	for key := range bextLoudness {
		known["bext."+key] = true
	}
	for _, key := range []string{"bext.timereference", "bext.version", "bext.umid", "bext.codinghistory", "ixml", "ixml.project", "ixml.scene", "ixml.take", "ixml.tape", "ixml.note", "cuesheet", "seektable", "comment:eng:", "lyrics:eng:", "url:", "rating:", "playcount:"} {
		known[key] = true
	}
	for _, key := range []string{"synchronizedlyrics", "eventtiming"} {
		known[key] = true
	}
	for key := range id3Names {
		known[key] = true
	}
	for key := range mp4Integers {
		known[key] = true
	}
	for key := range mp4Names {
		known[key] = true
	}
	for _, key := range []string{
		"author", "narrator", "language", "lyrics", "comment", "publisher",
		"tracktotal", "disctotal", "series", "seriespart", "isbn", "asin",
		"replaygain_track_gain", "replaygain_track_peak", "replaygain_album_gain",
		"replaygain_album_peak", "musicbrainz_trackid", "musicbrainz_albumid",
		"musicbrainz_artistid", "musicbrainz_albumartistid",
	} {
		known[key] = true
	}
	keys := make([]string, 0, len(known))
	for key := range known {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

package proxy

import "net/http"

// setCORS allows the FASTGen UI (and other browser clients) to audition
// /stream, /s, and /seg responses via hls.js XHR. Dialect behavior is unchanged.
func setCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Range, Content-Type, Accept, Origin")
	h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Content-Type")
}

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		next(w, r)
	}
}

func corsPreflight(w http.ResponseWriter, _ *http.Request) {
	setCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

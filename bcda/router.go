package main

import (
	"net/http"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/CMSgov/bcda-app/bcda/auth"
	"github.com/CMSgov/bcda-app/bcda/logging"
	"github.com/CMSgov/bcda-app/bcda/monitoring"
	"github.com/go-chi/chi"
)

func NewAPIRouter() http.Handler {
	r := chi.NewRouter()
	m := monitoring.GetMonitor()
	r.Use(auth.ParseToken, logging.NewStructuredLogger(), ConnectionClose)

	// TODO: serve up the static BCDA intro/info page

	if os.Getenv("ENABLE_API_ENDPOINTS") == "true" {

	// Serve up the swagger ui folder
	FileServer(r, "/api/v1/swagger", http.Dir("./swaggerui"))
	r.Get(m.WrapHandler("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("Hello world!"))
		if err != nil {
			log.Error(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}))
	r.Route("/api/v1", func(r chi.Router) {
		r.With(auth.RequireTokenAuth, ValidateBulkRequestHeaders).Get(m.WrapHandler("/ExplanationOfBenefit/$export", bulkEOBRequest))
		if os.Getenv("ENABLE_PATIENT_EXPORT") == "true" {
			r.With(auth.RequireTokenAuth, ValidateBulkRequestHeaders).Get(m.WrapHandler("/Patient/$export", bulkPatientRequest))
		}
		r.With(auth.RequireTokenAuth).Get(m.WrapHandler("/jobs/{jobId}", jobStatus))
		r.Get(m.WrapHandler("/metadata", metadata))
		if os.Getenv("DEBUG") == "true" {
			r.Get(m.WrapHandler("/token", getToken))
		}
	})
        }
    
        // Note: we want to serve up /_version and /_health no matter what
	r.Get(m.WrapHandler("/_version", getVersion))
	r.Get(m.WrapHandler("/_health", healthCheck))
	
	return r
}

func NewDataRouter() http.Handler {
	r := chi.NewRouter()
	m := monitoring.GetMonitor()
	r.Use(ConnectionClose)
	r.With(auth.ParseToken, logging.NewStructuredLogger(), auth.RequireTokenAuth,
		auth.RequireTokenACOMatch).Get(m.WrapHandler("/data/{jobID}/{acoID}.ndjson", serveData))
	return r
}

// FileServer conveniently sets up a http.FileServer handler to serve
// static files from a http.FileSystem.
// stolen from https://github.com/go-chi/chi/blob/master/_examples/fileserver/main.go
func FileServer(r chi.Router, path string, root http.FileSystem) {
	m := monitoring.GetMonitor()
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(m.WrapHandler(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	})))
}

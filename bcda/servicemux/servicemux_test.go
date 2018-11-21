package servicemux

import (
	"crypto/tls"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ServiceMuxTestSuite struct {
	suite.Suite
}

func (s *ServiceMuxTestSuite) TestNew() {
	sm := New("127.0.0.1:0")
	go func() {
		defer sm.Close()
	}()

	assert.NotNil(s.T(), sm)
	assert.Equal(s.T(), "127.0.0.1:0", sm.Addr)
	assert.NotNil(s.T(), sm.Listener)
	assert.IsType(s.T(), tcpKeepAliveListener{}, sm.Listener)
	assert.Empty(s.T(), sm.Servers)
}

func (s *ServiceMuxTestSuite) TestAddServer() {
	sm := New("127.0.0.1:0")
	go func() {
		defer sm.Close()
	}()

	srv := &http.Server{}
	go func() {
		defer srv.Close()
	}()
	sm.AddServer(srv, "test")

	assert.Len(s.T(), sm.Servers, 1)
	for k, v := range sm.Servers[0] {
		assert.Equal(s.T(), srv, k)
		assert.Equal(s.T(), "test", v)
	}
}

func (s *ServiceMuxTestSuite) TestServe_NoCert() {
	origTLSCert, origTLSKey, origHTTPOnly := getOrigVars()

	defer resetOrigVars(origTLSCert, origTLSKey, origHTTPOnly)

	os.Setenv("BCDA_TLS_CERT", "")
	os.Setenv("BCDA_TLS_KEY", "test.key")
	os.Setenv("HTTP_ONLY", "")

	sm := &ServiceMux{}
	assert.Panics(s.T(), sm.Serve)
}

func (s *ServiceMuxTestSuite) TestServe_NoKey() {
	origTLSCert, origTLSKey, origHTTPOnly := getOrigVars()

	defer resetOrigVars(origTLSCert, origTLSKey, origHTTPOnly)

	os.Setenv("BCDA_TLS_CERT", "test.crt")
	os.Setenv("BCDA_TLS_KEY", "")
	os.Setenv("HTTP_ONLY", "")

	sm := &ServiceMux{}
	assert.Panics(s.T(), sm.Serve)
}

var testHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte("Test"))
	if err != nil {
		log.Fatal(err)
	}
}

func (s *ServiceMuxTestSuite) TestServe_serveHTTPS() {
	srv := &http.Server{
		Handler: testHandler,
	}

	sm := New("127.0.0.1:0")
	sm.AddServer(srv, "/test")

	go func() {
		defer sm.Close()

		origTLSCert, origTLSKey, origHTTPOnly := getOrigVars()

		defer resetOrigVars(origTLSCert, origTLSKey, origHTTPOnly)

		os.Setenv("BCDA_TLS_CERT", "../../shared_files/localhost.crt")
		os.Setenv("BCDA_TLS_KEY", "../../shared_files/localhost.key")
		os.Setenv("HTTP_ONLY", "false")

		sm.Serve()
	}()

	// Allow certificate signed by unknown authority
	http.DefaultClient.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	resp, err := http.Get("https://" + sm.Listener.Addr().String() + "/test")
	if err != nil {
		s.T().Fatal(err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		s.T().Fatal(err)
	}

	assert.Equal(s.T(), "Test", string(body))
}

func (s *ServiceMuxTestSuite) TestServe_serveHTTPS_badKeypair() {
	srv := &http.Server{
		Handler: testHandler,
	}

	sm := New("127.0.0.1:0")
	sm.AddServer(srv, "/test")

	defer sm.Close()

	origTLSCert, origTLSKey, origHTTPOnly := getOrigVars()

	defer resetOrigVars(origTLSCert, origTLSKey, origHTTPOnly)

	os.Setenv("BCDA_TLS_CERT", "foo.crt")
	os.Setenv("BCDA_TLS_KEY", "foo.key")
	os.Setenv("HTTP_ONLY", "false")

	assert.Panics(s.T(), sm.Serve)
}

func (s *ServiceMuxTestSuite) TestServe_serveHTTP() {
	srv := http.Server{
		Handler: testHandler,
	}

	sm := New("127.0.0.1:0")
	sm.AddServer(&srv, "/test")

	go func() {
		origTLSCert, origTLSKey, origHTTPOnly := getOrigVars()

		defer func() {
			sm.Close()
			resetOrigVars(origTLSCert, origTLSKey, origHTTPOnly)
		}()

		os.Setenv("BCDA_TLS_CERT", "")
		os.Setenv("BCDA_TLS_KEY", "")
		os.Setenv("HTTP_ONLY", "true")

		sm.Serve()
	}()

	resp, err := http.Get("http://" + sm.Listener.Addr().String() + "/test")
	if err != nil {
		s.T().Fatal(err)
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		s.T().Fatal(err)
	}

	assert.Equal(s.T(), "Test", string(b))
}

func (s *ServiceMuxTestSuite) TestServe_serveHTTP_emptyPath() {
	srv := http.Server{
		Handler: testHandler,
	}

	sm := New("127.0.0.1:0")
	sm.AddServer(&srv, "")

	go func() {
		origTLSCert, origTLSKey, origHTTPOnly := getOrigVars()

		defer func() {
			sm.Close()
			resetOrigVars(origTLSCert, origTLSKey, origHTTPOnly)
		}()

		os.Setenv("BCDA_TLS_CERT", "")
		os.Setenv("BCDA_TLS_KEY", "")
		os.Setenv("HTTP_ONLY", "true")

		sm.Serve()
	}()

	resp, err := http.Get("http://" + sm.Listener.Addr().String() + "/foo")
	if err != nil {
		s.T().Fatal(err)
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		s.T().Fatal(err)
	}

	assert.Equal(s.T(), "Test", string(b))
}

func (s *ServiceMuxTestSuite) TestIsHTTPS_false() {
	req := httptest.NewRequest("GET", "/", nil)
	assert.False(s.T(), IsHTTPS(req))
}

func TestServiceMuxTestSuite(t *testing.T) {
	suite.Run(t, new(ServiceMuxTestSuite))
}

func getOrigVars() (origTLSCert, origTLSKey, origHTTPOnly string) {
	return os.Getenv("BCDA_TLS_CERT"), os.Getenv("BCDA_TLS_KEY"), os.Getenv("HTTP_ONLY")
}

func resetOrigVars(origTLSCert, origTLSKey, origHTTPOnly string) {
	os.Setenv("BCDA_TLS_CERT", origTLSCert)
	os.Setenv("BCDA_TLS_KEY", origTLSKey)
	os.Setenv("HTTP_ONLY", origHTTPOnly)
}
package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/spf13/viper"

	"github.com/kellegous/go/backend"
	"github.com/kellegous/go/internal"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
	"github.com/alexedwards/scs/v2"
)

var applicationID = "clientid"
var tenantID = "directoryid"
var clientSecret = "applicationpassword"
var baseAddr = "https://example.com" // w/o slash at the end

var SessionManager *scs.SessionManager

func InitSessionManager() {
	SessionManager = scs.New()
	SessionManager.Lifetime = time.Hour
}

// Serve a bundled asset over HTTP.
func serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	n, err := AssetInfo(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	a, err := Asset(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	http.ServeContent(w, r, n.Name(), n.ModTime(), bytes.NewReader(a))
}

func templateFromAssetFn(fn func() (*asset, error)) (*template.Template, error) {
	a, err := fn()
	if err != nil {
		return nil, err
	}

	t := template.New(a.info.Name())
	return t.Parse(string(a.bytes))
}

// The default handler responds to most requests. It is responsible for the
// shortcut redirects and for sending unmapped shortcuts to the edit page.
func getDefault(backend backend.Backend, w http.ResponseWriter, r *http.Request) {
	p := parseName("/", r.URL.Path)
	if p == "" {
		http.Redirect(w, r, "/edit/", http.StatusTemporaryRedirect)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	rt, err := backend.Get(ctx, p)
	if errors.Is(err, internal.ErrRouteNotFound) {
		http.Redirect(w, r,
			fmt.Sprintf("/edit/%s", cleanName(p)),
			http.StatusTemporaryRedirect)
		return
	} else if err != nil {
		log.Panic(err)
	}

	http.Redirect(w, r,
		rt.URL,
		http.StatusTemporaryRedirect)

}

func getLinks(backend backend.Backend, w http.ResponseWriter, r *http.Request) {
	t, err := templateFromAssetFn(linksHtml)
	if err != nil {
		log.Panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	rts, err := backend.GetAll(ctx, GetUserInfo(w, r, "azureId"))
	if err != nil {
		log.Panic(err)
	}

	if err := t.Execute(w, rts); err != nil {
		log.Panic(err)
	}
}

func randStr(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"
	var seededRand *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func GetUserInfo(w http.ResponseWriter, r *http.Request, name string) string {
	ad := SessionManager.GetString(r.Context(), name)
	if ad == "" {
		http.Redirect(w, r, "/login/"+randStr(10), http.StatusTemporaryRedirect)
	}
	return ad
}

// ListenAndServe sets up all web routes, binds the port and handles incoming
// web requests.
func ListenAndServe(backend backend.Backend) error {
	addr := viper.GetString("addr")
	admin := viper.GetBool("admin")
	version := viper.GetString("version")
	host := viper.GetString("host")

	mux := http.NewServeMux()

	mux.HandleFunc("/api/url/", func(w http.ResponseWriter, r *http.Request) {
		apiURL(backend, host, w, r)
	})
	mux.HandleFunc("/api/urls/", func(w http.ResponseWriter, r *http.Request) {
		apiURLs(backend, host, w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		getDefault(backend, w, r)
	})
	mux.HandleFunc("/edit/", func(w http.ResponseWriter, r *http.Request) {
		p := parseName("/edit/", r.URL.Path)

		// if this is a banned name, just redirect to the local URI. That'll show em.
		if isBannedName(p) {
			http.Redirect(w, r, fmt.Sprintf("/%s", p), http.StatusTemporaryRedirect)
			return
		}

		serveAsset(w, r, "edit.html")
	})
	mux.HandleFunc("/login/", func(w http.ResponseWriter, r *http.Request) {
		cred, err := confidential.NewCredFromSecret(clientSecret)
		if err != nil {
			fmt.Fprintln(w, "Application creation failure.")
		}
		app, err := confidential.New(applicationID, cred,
			confidential.WithAuthority("https://login.microsoftonline.com/"+
				tenantID))
		if err != nil {
			panic(err)
		}
		result, err := app.AuthCodeURL(context.Background(),
			applicationID,
			baseAddr+"/verify/",
			[]string{"User.Read"})
		if err != nil {
			log.Println(err)
		}
		http.Redirect(w, r, result, http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/verify/", func(w http.ResponseWriter, r *http.Request) {
		cred, err := confidential.NewCredFromSecret(clientSecret)
		if err != nil {
			fmt.Fprintln(w, "Application creation failure.")
		}
		app, err := confidential.New(applicationID, cred,
			confidential.WithAuthority("https://login.microsoftonline.com/"+
				tenantID))
		if err != nil {
			fmt.Fprintln(w, err)
		}
		key := r.URL.Query().Get("code")
		result, err := app.AcquireTokenByAuthCode(context.Background(), key,
			baseAddr+"/verify/",
			[]string{"User.Read"})
		if err != nil {
			fmt.Fprintln(w, err)
		}
		SessionManager.Put(r.Context(), "azureId", result.Account.HomeAccountID)
		SessionManager.Put(r.Context(), "name", result.Account.PreferredUsername)
		http.Redirect(w, r, "/?seed="+randStr(10), http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/reverify/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, GetUserInfo(w, r, "name"))
	})
	mux.HandleFunc("/links/", func(w http.ResponseWriter, r *http.Request) {
		getLinks(backend, w, r)
	})
	mux.HandleFunc("/s/", func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, r, r.URL.Path[len("/s/"):])
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, version)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "👍")
	})

	// TODO(knorton): Remove the admin handler.
	if admin {
		mux.Handle("/admin/", &adminHandler{backend})
	}

	return http.ListenAndServeTLS(addr, "cert.pem", "privkey.pem", SessionManager.LoadAndSave(mux))
}

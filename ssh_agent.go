package main

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh/agent"
)

const (
	oauthClientID     = "f4d36b7671004ed9850148fa645acac6"
	oauthClientSecret = "da475ea72e58427ab5c8a31e17ef2347"
	oauthTokenURL     = "https://oauth.yandex-team.ru/token"
)

func oauthLogin() string {
	if u := strings.TrimSpace(os.Getenv("YA_USER")); u != "" {
		return u
	}

	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}

	return os.Getenv("USER")
}

func tokenFromSSHAgent(login string) string {
	sock := os.Getenv("SSH_AUTH_SOCK")

	if sock == "" || login == "" {
		return ""
	}

	conn, err := net.Dial("unix", sock)

	if err != nil {
		return ""
	}

	defer conn.Close()

	ag := agent.NewClient(conn)
	keys, err := ag.List()

	if err != nil {
		return ""
	}

	ts := time.Now().Unix()
	data := []byte(strconv.FormatInt(ts, 10) + oauthClientID + login)

	for _, key := range keys {
		sig, err := ag.Sign(key, data)

		if err != nil {
			continue
		}

		form := url.Values{
			"grant_type":    {"ssh_key"},
			"client_id":     {oauthClientID},
			"client_secret": {oauthClientSecret},
			"login":         {login},
			"ts":            {strconv.FormatInt(ts, 10)},

			"ssh_sign": {base64.RawURLEncoding.EncodeToString(sig.Blob)},
		}

		if strings.Contains(key.Format, "cert") {
			form.Set("public_cert", base64.RawURLEncoding.EncodeToString(key.Blob))
		}

		if tok := postOAuthToken(form); tok != "" {
			return tok
		}
	}

	return ""
}

func postOAuthToken(form url.Values) string {
	resp, err := http.PostForm(oauthTokenURL, form)

	if err != nil {
		return ""
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}

	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return ""
	}

	return out.AccessToken
}

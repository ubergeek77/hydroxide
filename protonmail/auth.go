package protonmail

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/crypto/openpgp"
)

type authInfoReq struct {
	ClientID     string
	ClientSecret string
	Username     string
}

type AuthInfo struct {
	TwoFactor       int
	version         int
	modulus         string
	serverEphemeral string
	salt            string
	srpSession      string
}

type AuthInfoResp struct {
	resp
	AuthInfo
	Version         int
	Modulus         string
	ServerEphemeral string
	Salt            string
	SRPSession      string
}

func (resp *AuthInfoResp) authInfo() *AuthInfo {
	info := &resp.AuthInfo
	info.version = resp.Version
	info.modulus = resp.Modulus
	info.serverEphemeral = resp.ServerEphemeral
	info.salt = resp.Salt
	info.srpSession = resp.SRPSession
	return info
}

func (c *Client) AuthInfo(username string) (*AuthInfo, error) {
	reqData := &authInfoReq{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Username:     username,
	}

	req, err := c.newJSONRequest(http.MethodPost, "/auth/info", reqData)
	if err != nil {
		return nil, err
	}

	var respData AuthInfoResp
	if err := c.doJSON(req, &respData); err != nil {
		return nil, err
	}

	return respData.authInfo(), nil
}

type authReq struct {
	ClientID        string
	ClientSecret    string
	Username        string
	SRPSession      string
	ClientEphemeral string
	ClientProof     string
	TwoFactorCode   string
}

type PasswordMode int

const (
	PasswordSingle PasswordMode = 1
	PasswordTwo                 = 2
)

type Auth struct {
	ExpiresIn    int
	Scope        string
	UID          string `json:"Uid"`
	RefreshToken string
	EventID      string
	PasswordMode PasswordMode

	accessToken string
	privateKey  string
	keySalt     string
}

type authResp struct {
	resp
	Auth
	AccessToken string
	TokenType   string
	ServerProof string
	PrivateKey  string
	KeySalt     string
}

func (resp *authResp) auth() *Auth {
	auth := &resp.Auth
	auth.accessToken = resp.AccessToken
	auth.privateKey = resp.PrivateKey
	auth.keySalt = resp.KeySalt
	return auth
}

func (c *Client) Auth(username, password, twoFactorCode string, info *AuthInfo) (*Auth, error) {
	if info == nil {
		var err error
		if info, err = c.AuthInfo(username); err != nil {
			return nil, err
		}
	}

	proofs, err := srp([]byte(password), info)
	if err != nil {
		return nil, err
	}

	reqData := &authReq{
		ClientID:        c.ClientID,
		ClientSecret:    c.ClientSecret,
		Username:        username,
		SRPSession:      info.srpSession,
		ClientEphemeral: base64.StdEncoding.EncodeToString(proofs.clientEphemeral),
		ClientProof:     base64.StdEncoding.EncodeToString(proofs.clientProof),
		TwoFactorCode:   twoFactorCode,
	}

	req, err := c.newJSONRequest(http.MethodPost, "/auth", reqData)
	if err != nil {
		return nil, err
	}

	var respData authResp
	if err := c.doJSON(req, &respData); err != nil {
		return nil, err
	}

	if err := proofs.VerifyServerProof(respData.ServerProof); err != nil {
		return nil, err
	}

	return respData.auth(), nil
}

func (c *Client) Unlock(auth *Auth, password []byte) (openpgp.EntityList, error) {
	if auth.PasswordMode == PasswordSingle {
		keySalt, err := base64.StdEncoding.DecodeString(auth.keySalt)
		if err != nil {
			return nil, err
		}

		password, err = computeKeyPassword(password, keySalt)
		if err != nil {
			return nil, err
		}
	}

	keyRing, err := openpgp.ReadArmoredKeyRing(strings.NewReader(auth.privateKey))
	if err != nil {
		return nil, err
	}
	if len(keyRing) == 0 {
		return nil, errors.New("auth key ring is empty")
	}

	for _, e := range keyRing {
		if err := e.PrivateKey.Decrypt(password); err != nil {
			return nil, err
		}
	}

	c.uid = auth.UID
	c.accessToken = auth.accessToken
	c.keyRing = keyRing
	return keyRing, nil
}

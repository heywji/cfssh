package cfapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBase = "https://api.cloudflare.com/client/v4"

type Client struct {
	Token string
	Base  string
	HTTP  *http.Client
}

func New(token string) *Client {
	return &Client{
		Token: strings.TrimSpace(token),
		Base:  defaultBase,
		HTTP:  &http.Client{Timeout: 45 * time.Second},
	}
}

type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Zone struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AccountID string `json:"-"`
	Account   struct {
		ID string `json:"id"`
	} `json:"account"`
}

type Tunnel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Token  string `json:"token"`
	Status string `json:"status"`
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (c *Client) do(ctx context.Context, method, path string, body any, query url.Values) (envelope, error) {
	var buf io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return envelope{}, err
		}
		buf = bytes.NewReader(data)
	}
	endpoint := strings.TrimRight(c.Base, "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, buf)
	if err != nil {
		return envelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "cfssh/0.1")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return envelope{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return envelope{}, err
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return envelope{}, fmt.Errorf("Cloudflare 返回了无法解析的响应（HTTP %d）", resp.StatusCode)
	}
	if !env.Success {
		if len(env.Errors) == 0 {
			return env, fmt.Errorf("Cloudflare API 失败（HTTP %d）", resp.StatusCode)
		}
		parts := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			parts = append(parts, fmt.Sprintf("%d %s", e.Code, e.Message))
		}
		return env, fmt.Errorf("Cloudflare API: %s", strings.Join(parts, "; "))
	}
	return env, nil
}

func (c *Client) Verify(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil, nil)
	return err
}

func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	return listPages[Account](ctx, c, "/accounts")
}

func (c *Client) ListZones(ctx context.Context) ([]Zone, error) {
	zones, err := listPages[Zone](ctx, c, "/zones")
	if err != nil {
		return nil, err
	}
	for i := range zones {
		zones[i].AccountID = zones[i].Account.ID
	}
	return zones, nil
}

func listPages[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var all []T
	for page := 1; page <= 10; page++ {
		q := url.Values{}
		q.Set("page", fmt.Sprint(page))
		q.Set("per_page", "50")
		env, err := c.do(ctx, http.MethodGet, path, nil, q)
		if err != nil {
			return nil, err
		}
		var batch []T
		if len(env.Result) > 0 && string(env.Result) != "null" {
			if err := json.Unmarshal(env.Result, &batch); err != nil {
				return nil, err
			}
		}
		all = append(all, batch...)
		if env.ResultInfo.TotalPages == 0 || page >= env.ResultInfo.TotalPages {
			break
		}
	}
	return all, nil
}

// EnsureRemoteTunnel reuses a remotely managed tunnel or creates one.
// The returned token is what `cloudflared tunnel run --token` needs.
func (c *Client) EnsureRemoteTunnel(ctx context.Context, accountID, name string) (Tunnel, error) {
	q := url.Values{}
	q.Set("is_deleted", "false")
	q.Set("name", name)
	q.Set("per_page", "50")
	env, err := c.do(ctx, http.MethodGet, "/accounts/"+accountID+"/cfd_tunnel", nil, q)
	if err != nil {
		return Tunnel{}, err
	}
	var listed []Tunnel
	if err := json.Unmarshal(env.Result, &listed); err != nil {
		return Tunnel{}, err
	}
	for _, tun := range listed {
		if tun.Name != name {
			continue
		}
		token, err := c.tunnelToken(ctx, accountID, tun.ID)
		if err != nil {
			return Tunnel{}, fmt.Errorf("隧道 %s 已存在，但取不到运行令牌。如果它是在本机用 cert.pem 创建的，请在 Cloudflare 控制台删掉同名隧道后重试: %w", name, err)
		}
		tun.Token = token
		return tun, nil
	}
	env, err = c.do(ctx, http.MethodPost, "/accounts/"+accountID+"/cfd_tunnel", map[string]string{
		"name":       name,
		"config_src": "cloudflare",
	}, nil)
	if err != nil {
		return Tunnel{}, err
	}
	var created Tunnel
	if err := json.Unmarshal(env.Result, &created); err != nil {
		return Tunnel{}, err
	}
	if created.Token == "" {
		created.Token, err = c.tunnelToken(ctx, accountID, created.ID)
		if err != nil {
			return Tunnel{}, err
		}
	}
	return created, nil
}

func (c *Client) tunnelToken(ctx context.Context, accountID, tunnelID string) (string, error) {
	env, err := c.do(ctx, http.MethodGet, "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/token", nil, nil)
	if err != nil {
		return "", err
	}
	var asString string
	if err := json.Unmarshal(env.Result, &asString); err == nil && asString != "" {
		return asString, nil
	}
	var asObj struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(env.Result, &asObj); err == nil && asObj.Token != "" {
		return asObj.Token, nil
	}
	return "", fmt.Errorf("隧道令牌响应为空")
}

func (c *Client) PutSSHIngress(ctx context.Context, accountID, tunnelID, hostname, service string) error {
	body := map[string]any{
		"config": map[string]any{
			"ingress": []map[string]string{
				{"hostname": hostname, "service": service},
				{"service": "http_status:404"},
			},
		},
	}
	_, err := c.do(ctx, http.MethodPut, "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations", body, nil)
	return err
}

type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Comment string `json:"comment"`
}

func (c *Client) UpsertTunnelCNAME(ctx context.Context, zoneID, hostname, tunnelID string) error {
	target := tunnelID + ".cfargotunnel.com"
	records, err := c.recordsNamed(ctx, zoneID, hostname)
	if err != nil {
		return err
	}
	body := map[string]any{
		"type":    "CNAME",
		"name":    hostname,
		"content": target,
		"proxied": true,
		"ttl":     1,
		"comment": "cfssh",
	}
	for _, rec := range records {
		if rec.Type == "CNAME" && (strings.HasSuffix(rec.Content, ".cfargotunnel.com") || rec.Comment == "cfssh") {
			_, err = c.do(ctx, http.MethodPut, "/zones/"+zoneID+"/dns_records/"+rec.ID, body, nil)
			return err
		}
		return fmt.Errorf("%s 已有 %s 记录，没有改它。请换一个主机名前缀", hostname, rec.Type)
	}
	_, err = c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", body, nil)
	return err
}

func (c *Client) DeleteHostname(ctx context.Context, zoneID, hostname string) error {
	records, err := c.recordsNamed(ctx, zoneID, hostname)
	if err != nil {
		return err
	}
	for _, rec := range records {
		if rec.Type != "CNAME" || !strings.HasSuffix(rec.Content, ".cfargotunnel.com") {
			continue
		}
		if _, err := c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+rec.ID, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) recordsNamed(ctx context.Context, zoneID, hostname string) ([]dnsRecord, error) {
	q := url.Values{}
	q.Set("name", hostname)
	env, err := c.do(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records", nil, q)
	if err != nil {
		return nil, err
	}
	var records []dnsRecord
	if len(env.Result) == 0 || string(env.Result) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(env.Result, &records); err != nil {
		return nil, err
	}
	return records, nil
}

type accessApp struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Type   string `json:"type"`
}

type accessPolicy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AccessCredential is the Cloudflare Access service token a client sends
// instead of an email one-time code. ID is the token's API id. ClientID and
// Secret are what cloudflared access ssh --service-token-id/--service-token-secret use.
type AccessCredential struct {
	ID       string
	ClientID string
	Secret   string
}

func (c *Client) EnsureAccessSSH(ctx context.Context, accountID, hostname string, saved AccessCredential) (AccessCredential, error) {
	q := url.Values{}
	q.Set("domain", hostname)
	env, err := c.do(ctx, http.MethodGet, "/accounts/"+accountID+"/access/apps", nil, q)
	if err != nil {
		return AccessCredential{}, err
	}
	var apps []accessApp
	if len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, &apps); err != nil {
			return AccessCredential{}, err
		}
	}
	var appID string
	for _, app := range apps {
		if strings.EqualFold(app.Domain, hostname) {
			appID = app.ID
			break
		}
	}
	if appID == "" {
		body := map[string]any{
			"name":                 "cfssh " + hostname,
			"domain":               hostname,
			"type":                 "ssh",
			"session_duration":     "24h",
			"app_launcher_visible": false,
		}
		env, err = c.do(ctx, http.MethodPost, "/accounts/"+accountID+"/access/apps", body, nil)
		if err != nil {
			return AccessCredential{}, err
		}
		var created accessApp
		if err := json.Unmarshal(env.Result, &created); err != nil {
			return AccessCredential{}, err
		}
		appID = created.ID
	}
	cred, err := c.ensureServiceToken(ctx, accountID, saved)
	if err != nil {
		return AccessCredential{}, err
	}
	if err := c.ensureServicePolicy(ctx, accountID, appID, cred.ID); err != nil {
		return AccessCredential{}, err
	}
	return cred, nil
}

type serviceToken struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ClientID string `json:"client_id"`
	Secret   string `json:"client_secret"`
}

func (c *Client) ensureServiceToken(ctx context.Context, accountID string, saved AccessCredential) (AccessCredential, error) {
	env, err := c.do(ctx, http.MethodGet, "/accounts/"+accountID+"/access/service_tokens", nil, nil)
	if err != nil {
		return AccessCredential{}, fmt.Errorf("读取 Access 服务令牌失败。令牌需要 Access: Service Tokens 的编辑权限: %w", err)
	}
	var tokens []serviceToken
	if len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, &tokens); err != nil {
			return AccessCredential{}, err
		}
	}
	if saved.ID != "" && saved.ClientID != "" && saved.Secret != "" {
		for _, item := range tokens {
			if item.ID == saved.ID {
				return saved, nil
			}
		}
	}
	env, err = c.do(ctx, http.MethodPost, "/accounts/"+accountID+"/access/service_tokens", map[string]string{
		"name":     "cfssh",
		"duration": "8760h",
	}, nil)
	if err != nil {
		return AccessCredential{}, err
	}
	var created serviceToken
	if err := json.Unmarshal(env.Result, &created); err != nil {
		return AccessCredential{}, err
	}
	if created.ClientID == "" || created.Secret == "" {
		return AccessCredential{}, fmt.Errorf("Cloudflare 没有返回服务令牌的客户端密钥")
	}
	return AccessCredential{ID: created.ID, ClientID: created.ClientID, Secret: created.Secret}, nil
}

func (c *Client) ensureServicePolicy(ctx context.Context, accountID, appID, tokenID string) error {
	body := map[string]any{
		"name":       "cfssh-allow",
		"decision":   "non_identity",
		"precedence": 1,
		"include": []map[string]any{
			{"service_token": map[string]string{"token_id": tokenID}},
		},
	}
	env, err := c.do(ctx, http.MethodGet, "/accounts/"+accountID+"/access/apps/"+appID+"/policies", nil, nil)
	if err != nil {
		return err
	}
	var policies []accessPolicy
	if len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, &policies); err != nil {
			return err
		}
	}
	for _, policy := range policies {
		if policy.Name == "cfssh-allow" {
			_, err = c.do(ctx, http.MethodPut, "/accounts/"+accountID+"/access/apps/"+appID+"/policies/"+policy.ID, body, nil)
			return err
		}
	}
	_, err = c.do(ctx, http.MethodPost, "/accounts/"+accountID+"/access/apps/"+appID+"/policies", body, nil)
	return err
}

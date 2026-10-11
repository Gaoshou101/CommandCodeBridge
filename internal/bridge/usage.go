package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/url"
	"sync"
	"time"
)

type UsageWindow struct {
	Name    string     `json:"name"`
	Used    *float64   `json:"used"`
	Cap     *float64   `json:"cap"`
	ResetAt *time.Time `json:"reset_at"`
}
type Usage struct {
	Status            string        `json:"status"`
	HTTPStatus        int           `json:"http_status,omitempty"`
	Error             string        `json:"error,omitempty"`
	Unavailable       []string      `json:"unavailable"`
	Plan              string        `json:"plan"`
	SubscriptionStart *time.Time    `json:"subscription_start"`
	SubscriptionEnd   *time.Time    `json:"subscription_end"`
	Monthly           *float64      `json:"monthly_credits"`
	MonthlyUsed       *float64      `json:"monthly_used_credits"`
	Purchased         *float64      `json:"purchased_credits"`
	Free              *float64      `json:"free_credits"`
	Remaining         *float64      `json:"remaining_credits"`
	Windows           []UsageWindow `json:"windows"`
	TotalRequests     *int64        `json:"account_requests"`
	UpdatedAt         *time.Time    `json:"updated_at"`
	CheckedAt         *time.Time    `json:"checked_at"`
}
type usageCache struct {
	fingerprint string
	value       Usage
	done        chan struct{}
}

func shaKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}
func usageView(c *usageCache) any {
	if c == nil {
		return nil
	}
	return c.value
}
func floatField(j map[string]any, key string) *float64 {
	if f, ok := numeric(j[key]); ok && f >= 0 {
		return &f
	}
	return nil
}
func timestamp(v any) *time.Time {
	if f, ok := numeric(v); ok {
		if f > 1e12 {
			f /= 1000
		}
		if f > 0 {
			t := time.Unix(int64(f), 0).UTC()
			return &t
		}
	}
	if text := str(v); text != "" {
		if t, e := time.Parse(time.RFC3339, text); e == nil {
			return &t
		}
	}
	return nil
}
func (s *Service) credentialUsage(cid, callback string, refresh bool) (Usage, error) {
	if e := s.begin(); e != nil {
		return Usage{}, e
	}
	defer s.active.Done()
	deadline := time.Now().Add(15 * time.Second)
	for {
		s.mu.Lock()
		c, ok := s.creds[cid]
		if !ok || s.revoked[cid] {
			s.mu.Unlock()
			return Usage{}, fail(404, "credential not found")
		}
		if c.Disabled {
			s.mu.Unlock()
			return Usage{Status: "disabled", Windows: []UsageWindow{}, Unavailable: []string{}}, nil
		}
		fingerprint := shaKey(c.APIKey + "|" + c.ProxyURL + "|" + s.cfg.BaseURL)
		cached := s.usage[cid]
		if cached == nil || cached.fingerprint != fingerprint {
			cached = &usageCache{fingerprint: fingerprint, value: Usage{Status: "unknown", Windows: []UsageWindow{}, Unavailable: []string{}}}
			s.usage[cid] = cached
		}
		if cached.done != nil {
			ch := cached.done
			s.mu.Unlock()
			timer := time.NewTimer(time.Until(deadline))
			select {
			case <-ch:
				timer.Stop()
				continue
			case <-timer.C:
				return Usage{}, fail(504, "usage refresh wait timed out")
			case <-s.stopCh:
				timer.Stop()
				return Usage{}, fail(503, "plugin is shutting down")
			}
		}
		ttl := time.Duration(s.cfg.UsageRefreshSeconds) * time.Second
		if refresh {
			ttl = 10 * time.Second
		}
		if cached.value.Status == "error" {
			ttl = 5 * time.Minute
		}
		if cached.value.CheckedAt != nil && time.Since(*cached.value.CheckedAt) < ttl {
			v := cached.value
			s.mu.Unlock()
			return v, nil
		}
		cached.done = make(chan struct{})
		ch := cached.done
		old := cached.value
		s.mu.Unlock()
		value, account, err := s.fetchUsage(c, callback, deadline)
		now := time.Now().UTC()
		if err != nil {
			value = old
			value.Status = "error"
			value.HTTPStatus = statusOf(err)
			value.Error = safeError(redactError(err, c.APIKey))
		} else {
			// Keep the last known section when only part of the refresh failed.
			// Unavailable explicitly marks which displayed data may be stale.
			if contains(value.Unavailable, "credits") {
				value.Monthly, value.Purchased, value.Free, value.Remaining = old.Monthly, old.Purchased, old.Free, old.Remaining
				value.Windows = old.Windows
			}
			if contains(value.Unavailable, "subscription") {
				value.SubscriptionStart = old.SubscriptionStart
				value.SubscriptionEnd = old.SubscriptionEnd
				value.MonthlyUsed = old.MonthlyUsed
			}
			if contains(value.Unavailable, "summary") {
				value.TotalRequests = old.TotalRequests
				if value.SubscriptionStart != nil && old.SubscriptionStart != nil && value.SubscriptionStart.Equal(*old.SubscriptionStart) {
					value.MonthlyUsed = old.MonthlyUsed
				}
			}
			if value.Plan == "" {
				value.Plan = old.Plan
			}
			value.UpdatedAt = &now
		}
		value.CheckedAt = &now
		s.mu.Lock()
		if s.usage[cid] == cached && shaKey(s.creds[cid].APIKey+"|"+s.creds[cid].ProxyURL+"|"+s.cfg.BaseURL) == fingerprint && !s.creds[cid].Disabled {
			cached.value = value
			if account != "" {
				s.accounts[cid] = account
			}
		}
		cached.done = nil
		close(ch)
		s.mu.Unlock()
		return value, nil
	}
}
func (s *Service) fetchUsage(c Credential, callback string, deadline time.Time) (Usage, string, error) {
	v := Usage{Status: "ok", Windows: []UsageWindow{}, Unavailable: []string{}}
	who, e := s.getJSON("/alpha/whoami?limits=1", c, callback, deadline)
	if e != nil {
		return v, "", e
	}
	user := object(who["user"])
	org := object(who["org"])
	accountID := str(user["id"])
	orgID := str(org["id"])
	if accountID == "" && orgID == "" {
		return v, "", fail(502, "account identity response is unrecognized")
	}
	identity := accountID
	if orgID != "" {
		identity = "org:" + orgID
	}
	account := shaKey(identity)
	suffix := ""
	if orgID != "" {
		suffix = "?orgId=" + url.QueryEscape(orgID)
	}
	credits, creditsErr := s.getJSON("/alpha/billing/credits"+suffix, c, callback, deadline)
	if creditsErr == nil {
		j := object(credits["credits"])
		v.Monthly = floatField(j, "monthlyCredits")
		v.Purchased = floatField(j, "purchasedCredits")
		v.Free = floatField(j, "freeCredits")
		if v.Monthly != nil && v.Purchased != nil && v.Free != nil {
			n := *v.Monthly + *v.Purchased + *v.Free
			v.Remaining = &n
		}
		v.Plan = str(j["planId"])
		windows := object(credits["windowLimits"])
		for _, name := range []string{"fiveHour", "weekly", "monthly"} {
			if w := object(windows[name]); w != nil {
				v.Windows = append(v.Windows, UsageWindow{Name: name, Used: floatField(w, "used"), Cap: floatField(w, "cap"), ResetAt: timestamp(w["resetAt"])})
			}
		}
		if j == nil {
			v.Unavailable = append(v.Unavailable, "credits")
		}
	} else {
		if statusOf(creditsErr) == 401 || statusOf(creditsErr) == 403 {
			return v, account, creditsErr
		}
		v.Unavailable = append(v.Unavailable, "credits")
	}
	sub, subErr := s.getJSON("/alpha/billing/subscriptions"+suffix, c, callback, deadline)
	since := ""
	if subErr == nil {
		j := object(sub["data"])
		if j != nil {
			if plan := str(j["planId"]); plan != "" {
				v.Plan = plan
			}
			v.SubscriptionEnd = timestamp(j["currentPeriodEnd"])
			v.SubscriptionStart = timestamp(j["currentPeriodStart"])
			if v.SubscriptionStart != nil {
				since = v.SubscriptionStart.Format(time.RFC3339)
			}
		} else {
			v.Unavailable = append(v.Unavailable, "subscription")
		}
	} else {
		if statusOf(subErr) == 401 || statusOf(subErr) == 403 {
			return v, account, subErr
		}
		v.Unavailable = append(v.Unavailable, "subscription")
	}
	summaryPath := "/alpha/usage/summary"
	q := url.Values{}
	if orgID != "" {
		q.Set("orgId", orgID)
	}
	if since != "" {
		q.Set("since", since)
	}
	if len(q) > 0 {
		summaryPath += "?" + q.Encode()
	}
	if summary, summaryErr := s.getJSON(summaryPath, c, callback, deadline); summaryErr != nil {
		if statusOf(summaryErr) == 401 || statusOf(summaryErr) == 403 {
			return v, account, summaryErr
		}
		v.Unavailable = append(v.Unavailable, "summary")
	} else {
		// Only label this as the current billing month's usage when the
		// subscription supplied a valid start for the summary query.
		if v.SubscriptionStart != nil && (str(summary["periodBasis"]) == "" || str(summary["periodBasis"]) == "subscription") {
			v.MonthlyUsed = floatField(summary, "totalMonthlyCredits")
		}
		// The account API reports totalCount. Retain compatibility with older
		// responses, but never turn an absent or invalid counter into zero.
		value, present := summary["totalCount"]
		if !present {
			value = summary["totalRequests"]
		}
		if n, ok := numeric(value); ok && n >= 0 && n < float64(math.MaxInt64) && math.Trunc(n) == n {
			total := int64(n)
			v.TotalRequests = &total
		} else {
			v.Unavailable = append(v.Unavailable, "summary")
		}
	}
	if len(v.Unavailable) > 0 {
		v.Status = "partial"
	}
	if v.Monthly == nil && v.Purchased == nil && v.Free == nil && v.Plan == "" {
		return v, account, fail(502, "upstream returned no recognized balance or plan fields")
	}
	return v, account, nil
}
func (s *Service) startSampler() {
	s.mu.Lock()
	if s.samplerStarted || s.stopped {
		s.mu.Unlock()
		return
	}
	s.samplerStarted = true
	s.active.Add(1)
	stop := s.stopCh
	s.mu.Unlock()
	go func() {
		defer s.active.Done()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
			}
			s.mu.RLock()
			ids := []string{}
			for id, c := range s.creds {
				if !c.Disabled {
					ids = append(ids, id)
				}
			}
			interval := s.cfg.UsageRefreshSeconds
			s.mu.RUnlock()
			var group sync.WaitGroup
			slots := make(chan struct{}, 3)
			for _, cid := range ids {
				select {
				case slots <- struct{}{}:
				case <-stop:
					return
				}
				group.Add(1)
				go func(cid string) {
					defer group.Done()
					defer func() { <-slots }()
					_, _ = s.credentialUsage(cid, "", false)
				}(cid)
			}
			group.Wait()
			timer.Reset(time.Duration(interval) * time.Second)
		}
	}()
}

package bridge

import (
	"errors"
	"strings"
	"time"
)

var errStreamDone = errors.New("stream complete")

type Attempt struct{ Provider, ProviderSource string }
type LogEntry struct {
	CredentialScope                    string     `json:"credential_scope,omitempty"`
	AccountScope                       string     `json:"account_scope,omitempty"`
	ModelScope                         string     `json:"model_scope,omitempty"`
	ClientProtocol                     string     `json:"client_protocol,omitempty"`
	UpstreamProtocol                   string     `json:"upstream_protocol,omitempty"`
	ZDRRequested                       bool       `json:"zdr_requested"`
	UpstreamAttempted                  bool       `json:"upstream_attempted"`
	Source                             string     `json:"source,omitempty"`
	ID                                 string     `json:"id"`
	Time                               time.Time  `json:"time"`
	Model                              string     `json:"model"`
	UpstreamModel                      string     `json:"upstream_model"`
	CredentialID                       string     `json:"credential_id"`
	Credential                         string     `json:"credential"`
	Stream                             bool       `json:"stream"`
	Status                             int        `json:"status"`
	Error                              string     `json:"error,omitempty"`
	ErrorKind                          string     `json:"error_kind,omitempty"`
	DurationMS                         int64      `json:"duration_ms"`
	TTFTMS                             *int64     `json:"ttft_ms"`
	PromptTokens                       int64      `json:"prompt_tokens"`
	CompletionTokens                   int64      `json:"completion_tokens"`
	CachedTokens                       int64      `json:"cached_tokens"`
	CacheWriteTokens                   int64      `json:"cache_write_tokens"`
	ReasoningTokens                    int64      `json:"reasoning_tokens"`
	UsageReported                      bool       `json:"usage_reported"`
	Provider                           string     `json:"provider"`
	ProviderSource                     string     `json:"provider_source"`
	ReasoningFrom                      string     `json:"reasoning_from"`
	ReasoningTo                        string     `json:"reasoning_to"`
	StreamEnd                          string     `json:"stream_end"`
	UpstreamSkipped                    bool       `json:"upstream_skipped"`
	RetryAt                            *time.Time `json:"retry_at,omitempty"`
	OutputTokensPerSecond              *float64   `json:"output_tokens_per_second"`
	promptReported, completionReported bool
	epoch                              uint64
}
type Counters struct {
	BucketSeconds int       `json:"bucket_seconds,omitempty"`
	Time          time.Time `json:"time"`
	Model         string    `json:"model"`
	Requests      int64     `json:"requests"`
	Success       int64     `json:"success"`
	Failed        int64     `json:"failed"`
	Skipped       int64     `json:"skipped"`
	WithUsage     int64     `json:"with_usage"`
	Input         int64     `json:"input_tokens"`
	Output        int64     `json:"output_tokens"`
	Cached        int64     `json:"cached_tokens"`
	CacheWrite    int64     `json:"cache_write_tokens"`
	DurationMS    int64     `json:"duration_ms"`
	TTFTMS        int64     `json:"ttft_ms"`
	TTFTSamples   int64     `json:"ttft_samples"`
	SpeedTokens   int64     `json:"speed_tokens"`
	SpeedMS       int64     `json:"speed_ms"`
}

func (a *Counters) add(b Counters) {
	a.Requests += b.Requests
	a.Success += b.Success
	a.Failed += b.Failed
	a.Skipped += b.Skipped
	a.WithUsage += b.WithUsage
	a.Input += b.Input
	a.Output += b.Output
	a.Cached += b.Cached
	a.CacheWrite += b.CacheWrite
	a.DurationMS += b.DurationMS
	a.TTFTMS += b.TTFTMS
	a.TTFTSamples += b.TTFTSamples
	a.SpeedTokens += b.SpeedTokens
	a.SpeedMS += b.SpeedMS
}
func (s *Service) newLog(r ExecutorRequest, c Credential, m Model, start time.Time) LogEntry {
	s.mu.RLock()
	epoch := s.statisticsEpoch
	scope := credentialEvidenceScope(c, s.cfg)
	account := s.accounts[c.ID]
	zdr := s.cfg.ZDR
	s.mu.RUnlock()
	protocol := "chat-completions"
	// CPA can replace SourceFormat with the translated plugin input format.
	// The original HTTP path remains authoritative for client evidence.
	requestPath := str(r.Metadata["request_path"])
	if strings.HasSuffix(requestPath, "/responses") || strings.Contains(strings.ToLower(r.SourceFormat), "response") {
		protocol = "responses"
	} else if strings.HasSuffix(requestPath, "/messages") || r.Format == "claude" {
		protocol = "messages"
	}
	upstream := "chat-completions"
	if !contains(m.Endpoints, "/chat/completions") && contains(m.Endpoints, "/messages") {
		upstream = "messages"
	}
	return LogEntry{CredentialScope: scope, AccountScope: account, ModelScope: modelEvidenceScope(m), ClientProtocol: protocol, UpstreamProtocol: upstream, ZDRRequested: zdr, ID: id(), Time: start.UTC(), Model: r.Model, UpstreamModel: m.UpstreamID, CredentialID: c.ID, Credential: c.Label, Stream: r.Stream, Source: evidenceSource(r), Provider: "unknown", ProviderSource: "not_reported", epoch: epoch}
}
func (s *Service) record(entry LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		return
	}
	now := time.Now().UTC()
	entry.Time = now
	if entry.UsageReported && entry.CompletionTokens > 0 {
		ms := entry.DurationMS
		if entry.TTFTMS != nil {
			ms -= *entry.TTFTMS
		}
		if ms >= 500 {
			speed := float64(entry.CompletionTokens) * 1000 / float64(ms)
			entry.OutputTokensPerSecond = &speed
		}
	}
	s.logs = append(s.logs, entry)
	if len(s.logs) > s.cfg.LogRetention {
		s.logs = s.logs[len(s.logs)-s.cfg.LogRetention:]
	}
	if entry.epoch == s.statisticsEpoch {
		minute := now.Truncate(time.Minute)
		key := minute.Format(time.RFC3339) + "|" + entry.Model
		c := s.statistics[key]
		c.Time = minute
		c.Model = entry.Model
		c.Requests++
		if entry.Status < 400 {
			c.Success++
		} else {
			c.Failed++
		}
		if entry.UpstreamSkipped {
			c.Skipped++
		} else {
			c.DurationMS += entry.DurationMS
			if entry.UsageReported {
				c.WithUsage++
				c.Input += entry.PromptTokens
				c.Output += entry.CompletionTokens
				c.Cached += entry.CachedTokens
				c.CacheWrite += entry.CacheWriteTokens
			}
			if entry.TTFTMS != nil {
				c.TTFTMS += *entry.TTFTMS
				c.TTFTSamples++
			}
			if entry.OutputTokensPerSecond != nil {
				ms := entry.DurationMS
				if entry.TTFTMS != nil {
					ms -= *entry.TTFTMS
				}
				c.SpeedMS += ms
				c.SpeedTokens += entry.CompletionTokens
			}
		}
		s.statistics[key] = c
		s.markDirtyLocked(dirtyStatistics)
	}
	s.markDirtyLocked(dirtyLogs)
}
func contentStarted(j map[string]any) bool {
	for _, v := range list(j["choices"]) {
		d := object(object(v)["delta"])
		if str(d["content"]) != "" || str(d["reasoning_content"]) != "" || str(d["reasoning"]) != "" || str(d["refusal"]) != "" || len(list(d["tool_calls"])) > 0 || d["redacted_thinking"] != nil {
			return true
		}
	}
	return false
}
func observeMetadata(j map[string]any, entry *LogEntry, _ *Attempt) {
	if p := str(j["provider"]); p != "" {
		entry.Provider = p
		entry.ProviderSource = "provider"
	}
	for _, root := range []map[string]any{object(j["provider_metadata"]), object(j["providerMetadata"]), object(j["metadata"])} {
		if p := str(root["provider"]); p != "" {
			entry.Provider = p
			entry.ProviderSource = "metadata.provider"
		}
	}
	u := object(j["usage"])
	if u == nil {
		return
	}
	if v, ok := u["prompt_tokens"]; ok {
		entry.PromptTokens = number(v)
		entry.promptReported = true
	}
	if v, ok := u["completion_tokens"]; ok {
		entry.CompletionTokens = number(v)
		entry.completionReported = true
	}
	entry.UsageReported = entry.promptReported && entry.completionReported
	d := object(u["prompt_tokens_details"])
	if d["cached_tokens"] != nil {
		entry.CachedTokens = number(d["cached_tokens"])
	}
	if d["cache_write_tokens"] != nil {
		entry.CacheWriteTokens = number(d["cache_write_tokens"])
	}
	if u["cache_creation_input_tokens"] != nil {
		entry.CacheWriteTokens = number(u["cache_creation_input_tokens"])
	}
	d = object(u["completion_tokens_details"])
	if d["reasoning_tokens"] != nil {
		entry.ReasoningTokens = number(d["reasoning_tokens"])
	}
}
func (s *Service) logView(r ManagementRequest) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := []LogEntry{}
	search := strings.ToLower(r.Query.Get("search"))
	status := r.Query.Get("status")
	from, to := rangeTimes(r)
	for i := len(s.logs) - 1; i >= 0; i-- {
		row := s.logs[i]
		if (!from.IsZero() && row.Time.Before(from)) || (!to.IsZero() && !row.Time.Before(to)) {
			continue
		}
		if status == "success" && row.Status >= 400 || status == "error" && row.Status < 400 {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(row.Model+" "+row.Credential+" "+row.Error+" "+row.ErrorKind+" "+row.Provider), search) {
			continue
		}
		rows = append(rows, row)
	}
	offset := queryInt(r.Query.Get("offset"), 0, 0, 1000000)
	limit := queryInt(r.Query.Get("limit"), 50, 1, 200)
	total := len(rows)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return map[string]any{"total": total, "items": rows[offset:end]}
}
func rangeTimes(r ManagementRequest) (time.Time, time.Time) {
	from, _ := time.Parse(time.RFC3339, r.Query.Get("from"))
	to, _ := time.Parse(time.RFC3339, r.Query.Get("to"))
	return from, to
}
func (s *Service) statisticsView(r ManagementRequest) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	from, to := rangeTimes(r)
	total := Counters{}
	models := map[string]Counters{}
	resolution := 60
	for _, c := range s.statistics {
		seconds := c.BucketSeconds
		if seconds == 0 {
			seconds = 60
		}
		if (!from.IsZero() && !c.Time.Add(time.Duration(seconds)*time.Second).After(from)) || (!to.IsZero() && !c.Time.Before(to)) {
			continue
		}
		if seconds > resolution {
			resolution = seconds
		}
		total.add(c)
		v := models[c.Model]
		v.Model = c.Model
		v.add(c)
		models[c.Model] = v
	}
	var speed, hit, ttft *float64
	if total.SpeedMS > 0 {
		v := float64(total.SpeedTokens) * 1000 / float64(total.SpeedMS)
		speed = &v
	}
	if total.Input > 0 {
		v := float64(total.Cached) / float64(total.Input) * 100
		hit = &v
	}
	if total.TTFTSamples > 0 {
		v := float64(total.TTFTMS) / float64(total.TTFTSamples)
		ttft = &v
	}
	return map[string]any{"totals": total, "models": models, "output_tokens_per_second": speed, "cache_hit_percent": hit, "average_ttft_ms": ttft, "storage_error": s.storageError, "storage": s.storageStatusLocked(), "bucket_seconds": resolution, "range_note": "历史日级统计按重叠整日纳入，自定义范围边界可能扩大至 UTC 日"}
}

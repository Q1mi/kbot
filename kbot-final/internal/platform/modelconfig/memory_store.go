package modelconfig

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/util"
)

type MemoryStore struct {
	mu          sync.RWMutex
	accounts    map[string]*providerAccountRecord
	deployments map[string]*ModelDeployment
	profiles    map[string]*ModelProfile
	versions    map[string]*ModelProfileVersion
	bindings    map[string]*ProjectBinding
	usage       map[string]*memoryUsageReservation
}

type memoryUsageReservation struct {
	request       llm.ProjectQuotaRequest
	minute, month time.Time
	expiresAt     time.Time
	actualTokens  int
	actualCost    float64
	status        string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		accounts: make(map[string]*providerAccountRecord), deployments: make(map[string]*ModelDeployment),
		profiles: make(map[string]*ModelProfile), versions: make(map[string]*ModelProfileVersion),
		bindings: make(map[string]*ProjectBinding), usage: make(map[string]*memoryUsageReservation),
	}
}

var _ Store = (*MemoryStore)(nil)

func (s *MemoryStore) CreateProviderAccount(_ context.Context, v *providerAccountRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateProviderAPIKey(_ context.Context, id string, ciphertext []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.accounts[id]
	if !ok {
		return fmt.Errorf("provider account not found")
	}
	v.APIKeyCiphertext = append([]byte(nil), ciphertext...)
	v.HasAPIKey = true
	return nil
}
func (s *MemoryStore) ListProviderAccounts(_ context.Context, ws string) ([]*providerAccountRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*providerAccountRecord
	for _, v := range s.accounts {
		if v.WorkspaceID == ws {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *MemoryStore) GetProviderAccount(_ context.Context, id string) (*providerAccountRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.accounts[id]
	if !ok {
		return nil, fmt.Errorf("provider account not found")
	}
	return v, nil
}
func (s *MemoryStore) CreateDeployment(_ context.Context, v *ModelDeployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deployments[v.ID] = v
	return nil
}
func (s *MemoryStore) UpdateDeploymentPricing(
	_ context.Context, id string, inputPrice, outputPrice, cachedInputPrice float64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.deployments[id]
	if !ok {
		return fmt.Errorf("model deployment not found")
	}
	v.InputPricePerMillion = inputPrice
	v.OutputPricePerMillion = outputPrice
	v.CachedInputPricePerMillion = cachedInputPrice
	v.UpdatedAt = time.Now()
	return nil
}
func (s *MemoryStore) ListDeployments(_ context.Context, ws string) ([]*ModelDeployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*ModelDeployment
	for _, v := range s.deployments {
		if v.WorkspaceID == ws {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *MemoryStore) GetDeployment(_ context.Context, id string) (*ModelDeployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.deployments[id]
	if !ok {
		return nil, fmt.Errorf("model deployment not found")
	}
	return v, nil
}
func (s *MemoryStore) CreateProfile(_ context.Context, v *ModelProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[v.ID] = v
	return nil
}

func (s *MemoryStore) CreateProfileWithVersion(_ context.Context, p *ModelProfile, v *ModelProfileVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.profiles {
		if existing.WorkspaceID == p.WorkspaceID && existing.Name == p.Name {
			return ErrProfileNameExists
		}
	}
	s.profiles[p.ID] = p
	s.versions[v.ID] = v
	return nil
}
func (s *MemoryStore) ListProfiles(_ context.Context, ws string) ([]*ModelProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*ModelProfile
	for _, v := range s.profiles {
		if v.WorkspaceID == ws {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *MemoryStore) GetProfile(_ context.Context, id string) (*ModelProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.profiles[id]
	if !ok {
		return nil, fmt.Errorf("model profile not found")
	}
	return v, nil
}
func (s *MemoryStore) CreateProfileVersion(_ context.Context, v *ModelProfileVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[v.ID] = v
	return nil
}
func (s *MemoryStore) ListProfileVersions(_ context.Context, profileID string) ([]*ModelProfileVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*ModelProfileVersion
	for _, v := range s.versions {
		if v.ProfileID == profileID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}
func (s *MemoryStore) GetProfileVersion(_ context.Context, id string) (*ModelProfileVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.versions[id]
	if !ok {
		return nil, fmt.Errorf("model profile version not found")
	}
	return v, nil
}
func (s *MemoryStore) UpsertProjectBinding(_ context.Context, v *ProjectBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[v.WorkspaceID+"@"+v.Env] = v
	return nil
}

func (s *MemoryStore) GetProjectBinding(_ context.Context, workspaceID, env string) (*ProjectBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bindings[workspaceID+"@"+env]
	if !ok {
		return nil, fmt.Errorf("project model binding not found")
	}
	copy := *b
	return &copy, nil
}

func (s *MemoryStore) ReserveProjectUsage(_ context.Context, req llm.ProjectQuotaRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[req.WorkspaceID+"@"+req.Env]
	if !ok {
		return "", nil
	}
	if binding.ModelProfileVersionID != req.ModelProfileVersionID {
		return "", fmt.Errorf("%w: profile is not bound to workspace environment %s", llm.ErrProjectQuotaExceeded, req.Env)
	}
	now := time.Now().UTC()
	minute := now.Truncate(time.Minute)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	calls, tokens, cost := 0, 0, float64(0)
	for _, usage := range s.usage {
		if usage.request.WorkspaceID != req.WorkspaceID || usage.request.Env != req.Env {
			continue
		}
		if usage.status == "reserved" && !usage.expiresAt.After(now) {
			usage.status = "failed"
			usage.actualTokens = 0
			usage.actualCost = 0
		}
		effectiveTokens, effectiveCost := usage.actualTokens, usage.actualCost
		if usage.status == "reserved" {
			effectiveTokens, effectiveCost = usage.request.ReservedTokens, usage.request.ReservedCost
		} else if usage.status == "completed" {
			effectiveTokens, effectiveCost = usage.actualTokens, usage.actualCost
		}
		if usage.minute.Equal(minute) {
			calls++
			tokens += effectiveTokens
		}
		if usage.month.Equal(month) {
			cost += effectiveCost
		}
	}
	if binding.RPMLimit > 0 && calls+1 > binding.RPMLimit {
		return "", fmt.Errorf("%w: RPM limit %d reached", llm.ErrProjectQuotaExceeded, binding.RPMLimit)
	}
	if binding.TPMLimit > 0 && tokens+req.ReservedTokens > binding.TPMLimit {
		return "", fmt.Errorf("%w: TPM limit %d would be exceeded", llm.ErrProjectQuotaExceeded, binding.TPMLimit)
	}
	if binding.MonthlyBudget > 0 && cost+req.ReservedCost > binding.MonthlyBudget {
		return "", fmt.Errorf("%w: monthly budget %.4f would be exceeded", llm.ErrProjectQuotaExceeded, binding.MonthlyBudget)
	}
	id := util.GenerateID()
	s.usage[id] = &memoryUsageReservation{
		request: req, minute: minute, month: month, expiresAt: now.Add(time.Hour), status: "reserved",
	}
	return id, nil
}

func (s *MemoryStore) FinalizeProjectUsage(_ context.Context, id string, actualTokens int, actualCost float64, success bool) error {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	usage, ok := s.usage[id]
	if !ok || usage.status != "reserved" {
		return fmt.Errorf("project model usage reservation is unavailable")
	}
	usage.actualTokens, usage.actualCost = actualTokens, actualCost
	usage.status = "failed"
	if success {
		usage.status = "completed"
	}
	return nil
}

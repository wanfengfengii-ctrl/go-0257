package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_CreateTaskConcurrentIdempotency(t *testing.T) {
	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "concurrent identical operation replays the committed task",
			run: func(t *testing.T) {
				backing := store.NewMemory()
				barrier := &modelFindBarrierStore{
					Store:       backing,
					operationID: "op-model-concurrent-create",
					parties:     2,
					release:     make(chan struct{}),
				}
				handler := modelCreateHandler(barrier)
				req := modelCreateRequest("op-model-concurrent-create", "lot-model-concurrent", "blind-model-concurrent")

				start := make(chan struct{})
				results := make([]modelCreateResult, 2)
				var wg sync.WaitGroup
				for i := range results {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						<-start
						results[i] = modelPostCreate(t, handler, req)
					}(i)
				}
				close(start)
				wg.Wait()

				for i, result := range results {
					if result.status != http.StatusCreated {
						t.Fatalf("request %d: expected status 201, got %d with error %s", i, result.status, result.errorCode)
					}
				}
				if results[0].response != results[1].response {
					t.Fatalf("expected identical task responses, got %#v and %#v", results[0].response, results[1].response)
				}
				tasks, err := backing.ListTasks()
				if err != nil {
					t.Fatalf("list tasks: %v", err)
				}
				if len(tasks) != 1 {
					t.Fatalf("expected exactly one created task, got %d", len(tasks))
				}
				if string(tasks[0].ID) != results[0].response.TaskID {
					t.Fatalf("created task %s does not match response %s", tasks[0].ID, results[0].response.TaskID)
				}
			},
		},
		{
			name: "same operation with different payload conflicts",
			run: func(t *testing.T) {
				handler := modelCreateHandler(store.NewMemory())
				first := modelCreateRequest("op-model-payload-conflict", "lot-model-payload-a", "blind-model-payload-a")
				if result := modelPostCreate(t, handler, first); result.status != http.StatusCreated {
					t.Fatalf("first create: expected status 201, got %d with error %s", result.status, result.errorCode)
				}

				second := modelCreateRequest("op-model-payload-conflict", "lot-model-payload-b", "blind-model-payload-b")
				result := modelPostCreate(t, handler, second)
				if result.status != http.StatusUnprocessableEntity {
					t.Fatalf("conflicting create: expected status 422, got %d", result.status)
				}
				if result.errorCode != string(domain.CodeIdempotencyConflict) {
					t.Fatalf("expected %s, got %s", domain.CodeIdempotencyConflict, result.errorCode)
				}
			},
		},
		{
			name: "different operation with real seed lot conflict remains rejected",
			run: func(t *testing.T) {
				handler := modelCreateHandler(store.NewMemory())
				first := modelCreateRequest("op-model-resource-a", "lot-model-resource", "blind-model-resource-a")
				if result := modelPostCreate(t, handler, first); result.status != http.StatusCreated {
					t.Fatalf("first create: expected status 201, got %d with error %s", result.status, result.errorCode)
				}

				second := modelCreateRequest("op-model-resource-b", "lot-model-resource", "blind-model-resource-b")
				result := modelPostCreate(t, handler, second)
				if result.status != http.StatusUnprocessableEntity {
					t.Fatalf("resource conflict: expected status 422, got %d", result.status)
				}
				if result.errorCode != string(domain.CodeOccupancyConflict) {
					t.Fatalf("expected %s, got %s", domain.CodeOccupancyConflict, result.errorCode)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

type modelFindBarrierStore struct {
	store.Store
	operationID string
	parties     int
	release     chan struct{}
	mu          sync.Mutex
	arrived     int
}

func (s *modelFindBarrierStore) FindOperation(op string) (*inspection.IdempotencyRecord, bool) {
	rec, ok := s.Store.FindOperation(op)
	if op != s.operationID {
		return rec, ok
	}

	s.mu.Lock()
	s.arrived++
	if s.arrived == s.parties {
		close(s.release)
	}
	release := s.release
	s.mu.Unlock()

	<-release
	return rec, ok
}

type modelCreateResult struct {
	status    int
	response  api.CreateTaskResponse
	errorCode string
}

func modelCreateHandler(st store.Store) http.Handler {
	c, roles := catalog.Seed()
	svc := api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
	return api.NewServer(svc, ".").Handler()
}

func modelCreateRequest(operationID, seedLot, blindCode string) api.CreateTaskRequest {
	return api.CreateTaskRequest{
		OperationID: operationID,
		SeedLot:     seedLot,
		Field:       "field-01",
		Variety:     "xiangliangyou-900",
		FemaleCert:  3,
		MaleCert:    3,
		BlindAllocs: []api.BlindAllocInput{
			{Code: blindCode, Germination: 100, Pathogen: 50, Moisture: 30},
		},
		Chamber:        "ch-" + blindCode,
		ChamberStart:   100,
		ChamberEnd:     200,
		Plate:          "plate-" + blindCode,
		Wells:          []string{"w1"},
		ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
	}
}

func modelPostCreate(t *testing.T, handler http.Handler, req api.CreateTaskRequest) modelCreateResult {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rr := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
	handler.ServeHTTP(rr, httpReq)

	result := modelCreateResult{status: rr.Code}
	if rr.Code == http.StatusCreated {
		if err := json.Unmarshal(rr.Body.Bytes(), &result.response); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		return result
	}

	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	result.errorCode = envelope.Error.Code
	return result
}

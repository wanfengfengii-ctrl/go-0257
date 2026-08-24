package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_CreateTaskDuplicateSeedLotRollback(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "riceguard.db")
	c, roles := catalog.Seed()
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()

	handler := api.NewServer(
		api.NewService(c, roles, st, pathogen.NewStaticAmplifier(), measure.NewScriptedMeter()),
		"",
	).Handler()

	firstReq := modelValidCreate("op-create-primary", "lot-1001", "b1", "ch-1", "p-1", "w1")
	var firstResp api.CreateTaskResponse

	cases := []struct {
		name          string
		req           func() api.CreateTaskRequest
		wantStatus    int
		wantError     domain.ErrorCode
		wantTaskCount int
		wantAudit     int
		check         func(t *testing.T, resp api.CreateTaskResponse)
	}{
		{
			name:          "first legal create succeeds",
			req:           func() api.CreateTaskRequest { return firstReq },
			wantStatus:    http.StatusCreated,
			wantTaskCount: 1,
			wantAudit:     1,
			check: func(t *testing.T, resp api.CreateTaskResponse) {
				if resp.TaskID == "" {
					t.Fatal("create response did not include task_id")
				}
				firstResp = resp
			},
		},
		{
			name:          "same operation and same content returns original response",
			req:           func() api.CreateTaskRequest { return firstReq },
			wantStatus:    http.StatusCreated,
			wantTaskCount: 1,
			wantAudit:     1,
			check: func(t *testing.T, resp api.CreateTaskResponse) {
				if !reflect.DeepEqual(resp, firstResp) {
					t.Fatalf("idempotent retry response changed: got %+v want %+v", resp, firstResp)
				}
			},
		},
		{
			name: "same operation and different content conflicts",
			req: func() api.CreateTaskRequest {
				return modelValidCreate("op-create-primary", "lot-2002", "b2", "ch-2", "p-2", "w2")
			},
			wantStatus:    http.StatusUnprocessableEntity,
			wantError:     domain.CodeIdempotencyConflict,
			wantTaskCount: 1,
			wantAudit:     1,
		},
		{
			name: "catalog validation error is not recorded as an operation",
			req: func() api.CreateTaskRequest {
				req := modelValidCreate("op-catalog-before-tx", "lot-2002", "b2", "ch-2", "p-2", "w2")
				req.Variety = "missing-variety"
				return req
			},
			wantStatus:    http.StatusUnprocessableEntity,
			wantError:     domain.CodeVarietyMismatch,
			wantTaskCount: 1,
			wantAudit:     1,
		},
		{
			name: "operation id can be reused after catalog validation rejection",
			req: func() api.CreateTaskRequest {
				return modelValidCreate("op-catalog-before-tx", "lot-2002", "b2", "ch-2", "p-2", "w2")
			},
			wantStatus:    http.StatusCreated,
			wantTaskCount: 2,
			wantAudit:     2,
		},
		{
			name: "duplicate seed lot with new operation is rejected and rolled back",
			req: func() api.CreateTaskRequest {
				return modelValidCreate("op-duplicate-seed", "lot-1001", "b3", "ch-3", "p-3", "w3")
			},
			wantStatus:    http.StatusUnprocessableEntity,
			wantError:     domain.CodeOccupancyConflict,
			wantTaskCount: 2,
			wantAudit:     2,
		},
		{
			name: "duplicate seed lot retry is still rejected instead of replaying a phantom success",
			req: func() api.CreateTaskRequest {
				return modelValidCreate("op-duplicate-seed", "lot-1001", "b3", "ch-3", "p-3", "w3")
			},
			wantStatus:    http.StatusUnprocessableEntity,
			wantError:     domain.CodeOccupancyConflict,
			wantTaskCount: 2,
			wantAudit:     2,
		},
		{
			name: "duplicate seed lot rollback did not persist an idempotency operation",
			req: func() api.CreateTaskRequest {
				req := modelValidCreate("op-duplicate-seed", "lot-3003", "b4", "ch-4", "p-4", "w4")
				req.Variety = "missing-variety"
				return req
			},
			wantStatus:    http.StatusUnprocessableEntity,
			wantError:     domain.CodeVarietyMismatch,
			wantTaskCount: 2,
			wantAudit:     2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, resp, gotError := modelPostCreate(t, handler, tc.req())
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d; response=%+v error=%s", status, tc.wantStatus, resp, gotError)
			}
			if gotError != tc.wantError {
				t.Fatalf("error code = %s, want %s", gotError, tc.wantError)
			}
			if tc.check != nil {
				tc.check(t, resp)
			}
			if got := modelTaskCount(t, handler); got != tc.wantTaskCount {
				t.Fatalf("task count = %d, want %d", got, tc.wantTaskCount)
			}
			if got := modelAuditCount(t, handler); got != tc.wantAudit {
				t.Fatalf("audit count = %d, want %d", got, tc.wantAudit)
			}
		})
	}
}

func modelValidCreate(op, seedLot, blindCode, chamber, plate, well string) api.CreateTaskRequest {
	return api.CreateTaskRequest{
		OperationID: op,
		SeedLot:     seedLot,
		Field:       "field-01",
		Variety:     "xiangliangyou-900",
		FemaleCert:  3,
		MaleCert:    3,
		BlindAllocs: []api.BlindAllocInput{
			{Code: blindCode, Germination: 100, Pathogen: 50, Moisture: 30},
		},
		Chamber:        chamber,
		ChamberStart:   100,
		ChamberEnd:     200,
		Plate:          plate,
		Wells:          []string{well},
		ReviewerRoster: []string{"reviewer-f", "reviewer-g"},
	}
}

func modelPostCreate(t *testing.T, handler http.Handler, req api.CreateTaskRequest) (int, api.CreateTaskResponse, domain.ErrorCode) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal create request: %v", err)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body)))

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response body %q: %v", rr.Body.String(), err)
	}
	if rawErr, ok := envelope["error"]; ok {
		var payload struct {
			Code domain.ErrorCode `json:"code"`
		}
		if err := json.Unmarshal(rawErr, &payload); err != nil {
			t.Fatalf("decode error response %q: %v", rr.Body.String(), err)
		}
		return rr.Code, api.CreateTaskResponse{}, payload.Code
	}
	var resp api.CreateTaskResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response %q: %v", rr.Body.String(), err)
	}
	return rr.Code, resp, domain.CodeNone
}

func modelTaskCount(t *testing.T, handler http.Handler) int {
	t.Helper()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list tasks status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode task list %q: %v", rr.Body.String(), err)
	}
	return len(payload.Tasks)
}

func modelAuditCount(t *testing.T, handler http.Handler) int {
	t.Helper()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/audit", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list audit status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Audit []json.RawMessage `json:"audit"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode audit list %q: %v", rr.Body.String(), err)
	}
	return len(payload.Audit)
}

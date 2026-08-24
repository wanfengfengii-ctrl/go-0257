package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/domain"
	"riceguard/inspection"
)

func TestModel_PathogenPlateWellCoverage(t *testing.T) {
	read := func(v int32) *int32 { return &v }
	setupPathogen := func(t *testing.T, svc *api.Service, wells []string, op string) string {
		t.Helper()
		req := validCreate(op + "-create")
		req.Wells = append([]string(nil), wells...)
		created, derr := svc.CreateTask(req)
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		id := created.TaskID
		if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
			OperationID: op + "-sampling-a", Reviewer: "sampler-a", Field: "field-01",
			SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180,
		}); derr != nil {
			t.Fatalf("sampling a: %v", derr)
		}
		if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
			OperationID: op + "-sampling-b", Reviewer: "sampler-b", Field: "field-01",
			SeedLot: "lot-1001", BlindSeal: "seal-1", SampleCount: 180,
		}); derr != nil {
			t.Fatalf("sampling b: %v", derr)
		}
		if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
			t.Fatalf("split: %v", derr)
		}
		if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"}); derr != nil {
			t.Fatalf("occupy: %v", derr)
		}
		for _, d := range []struct {
			age int32
			op  string
		}{
			{age: 2, op: "-g2"},
			{age: 5, op: "-g5"},
			{age: 8, op: "-g8"},
		} {
			if _, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: op + d.op, BlindCode: "b1", DayAge: d.age,
				Normal: 95, Abnormal: 3, Dead: 2, Collector: "germinator-c",
			}); derr != nil {
				t.Fatalf("germination day %d: %v", d.age, derr)
			}
		}
		view, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get task: %v", derr)
		}
		if view.Task.Status != inspection.StatusPathogen {
			t.Fatalf("expected pathogen_checking after setup, got %s", view.Task.Status)
		}
		return id
	}
	assertView := func(t *testing.T, svc *api.Service, id string, want inspection.TaskStatus, wantEvidence int) {
		t.Helper()
		view, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get task: %v", derr)
		}
		if view.Task.Status != want {
			t.Fatalf("expected status %s, got %s", want, view.Task.Status)
		}
		if len(view.Pathogen) != wantEvidence {
			t.Fatalf("expected %d pathogen evidence rows, got %d", wantEvidence, len(view.Pathogen))
		}
	}

	cases := []struct {
		name  string
		op    string
		wells []string
		run   func(t *testing.T, svc *api.Service, id string)
	}{
		{
			name:  "single well advances after its valid reading",
			op:    "model-single",
			wells: []string{"w1"},
			run: func(t *testing.T, svc *api.Service, id string) {
				resp, derr := svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-single-p1", BlindCode: "b1", Plate: "p-1", Well: "w1",
					Verifier: "pathologist-d", Reading: read(10),
				})
				if derr != nil {
					t.Fatalf("pathogen w1: %v", derr)
				}
				if !resp.Advanced || resp.Status != inspection.StatusMoisture {
					t.Fatalf("expected single well to advance to moisture_checking, got advanced=%v status=%s", resp.Advanced, resp.Status)
				}
				assertView(t, svc, id, inspection.StatusMoisture, 1)
			},
		},
		{
			name:  "multi well waits for every locked well",
			op:    "model-multi",
			wells: []string{"w1", "w2"},
			run: func(t *testing.T, svc *api.Service, id string) {
				resp, derr := svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-multi-p1", BlindCode: "b1", Plate: "p-1", Well: "w1",
					Verifier: "pathologist-d", Reading: read(10),
				})
				if derr != nil {
					t.Fatalf("pathogen w1: %v", derr)
				}
				if resp.Advanced || resp.Status != inspection.StatusPathogen {
					t.Fatalf("expected w1 alone to stay in pathogen_checking, got advanced=%v status=%s", resp.Advanced, resp.Status)
				}
				assertView(t, svc, id, inspection.StatusPathogen, 1)

				resp, derr = svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-multi-p2", BlindCode: "b1", Plate: "p-1", Well: "w2",
					Verifier: "pathologist-d", Reading: read(10),
				})
				if derr != nil {
					t.Fatalf("pathogen w2: %v", derr)
				}
				if !resp.Advanced || resp.Status != inspection.StatusMoisture {
					t.Fatalf("expected all locked wells to advance to moisture_checking, got advanced=%v status=%s", resp.Advanced, resp.Status)
				}
				assertView(t, svc, id, inspection.StatusMoisture, 2)
			},
		},
		{
			name:  "unknown well is rejected",
			op:    "model-unknown",
			wells: []string{"w1", "w2"},
			run: func(t *testing.T, svc *api.Service, id string) {
				_, derr := svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-unknown-p3", BlindCode: "b1", Plate: "p-1", Well: "w3",
					Verifier: "pathologist-d", Reading: read(10),
				})
				if derr == nil {
					t.Fatal("expected unknown well rejection, got nil")
				}
				if derr.Code != domain.CodeBadRequest {
					t.Fatalf("expected %s, got %s", domain.CodeBadRequest, derr.Code)
				}
				assertView(t, svc, id, inspection.StatusPathogen, 0)
			},
		},
		{
			name:  "covered well duplicate is rejected before remaining well is read",
			op:    "model-duplicate",
			wells: []string{"w1", "w2"},
			run: func(t *testing.T, svc *api.Service, id string) {
				if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-duplicate-p1", BlindCode: "b1", Plate: "p-1", Well: "w1",
					Verifier: "pathologist-d", Reading: read(10),
				}); derr != nil {
					t.Fatalf("pathogen w1: %v", derr)
				}
				_, derr := svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-duplicate-p1-again", BlindCode: "b1", Plate: "p-1", Well: "w1",
					Verifier: "pathologist-d", Reading: read(10),
				})
				if derr == nil {
					t.Fatal("expected duplicate well rejection, got nil")
				}
				if derr.Code != domain.CodeBadRequest {
					t.Fatalf("expected %s, got %s", domain.CodeBadRequest, derr.Code)
				}
				assertView(t, svc, id, inspection.StatusPathogen, 1)
			},
		},
		{
			name:  "late isolated evidence does not cover a locked well",
			op:    "model-late",
			wells: []string{"w1", "w2"},
			run: func(t *testing.T, svc *api.Service, id string) {
				if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-late-p1", BlindCode: "b1", Plate: "p-1", Well: "w1",
					Verifier: "pathologist-d", Generation: 1, Reading: read(10),
				}); derr != nil {
					t.Fatalf("late pathogen w1: %v", derr)
				}
				assertView(t, svc, id, inspection.StatusPathogen, 1)

				resp, derr := svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-late-p2", BlindCode: "b1", Plate: "p-1", Well: "w2",
					Verifier: "pathologist-d", Reading: read(10),
				})
				if derr != nil {
					t.Fatalf("pathogen w2: %v", derr)
				}
				if resp.Advanced || resp.Status != inspection.StatusPathogen {
					t.Fatalf("expected late w1 not to cover locked well, got advanced=%v status=%s", resp.Advanced, resp.Status)
				}
				assertView(t, svc, id, inspection.StatusPathogen, 2)

				resp, derr = svc.RecordPathogen(id, api.PathogenRequest{
					OperationID: "model-late-p1-current", BlindCode: "b1", Plate: "p-1", Well: "w1",
					Verifier: "pathologist-d", Reading: read(10),
				})
				if derr != nil {
					t.Fatalf("current pathogen w1: %v", derr)
				}
				if !resp.Advanced || resp.Status != inspection.StatusMoisture {
					t.Fatalf("expected current evidence for both wells to advance, got advanced=%v status=%s", resp.Advanced, resp.Status)
				}
				assertView(t, svc, id, inspection.StatusMoisture, 3)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := seedService()
			id := setupPathogen(t, svc, tc.wells, tc.op)
			tc.run(t, svc, id)
		})
	}
}

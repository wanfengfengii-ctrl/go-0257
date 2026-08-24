package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/domain"
	"riceguard/inspection"
)

func TestModel_BlindCodeReleaseUnblindIsGlobal(t *testing.T) {
	driveToReleasableWithCode := func(t *testing.T, svc *api.Service, opPrefix, seedLot, code string) string {
		t.Helper()

		create := validCreate(opPrefix + "-create")
		create.SeedLot = seedLot
		create.BlindAllocs = []api.BlindAllocInput{
			{Code: code, Germination: 100, Pathogen: 50, Moisture: 30},
		}
		create.Chamber = "ch-" + opPrefix
		create.Plate = "p-" + opPrefix

		resp, derr := svc.CreateTask(create)
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		id := resp.TaskID

		for _, confirmation := range []struct {
			op       string
			reviewer string
		}{
			{op: "s1", reviewer: "sampler-a"},
			{op: "s2", reviewer: "sampler-b"},
		} {
			if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
				OperationID: opPrefix + "-" + confirmation.op,
				Reviewer:    confirmation.reviewer,
				Field:       "field-01",
				SeedLot:     seedLot,
				BlindSeal:   "seal-" + opPrefix,
				SampleCount: 180,
			}); derr != nil {
				t.Fatalf("sampling %s: %v", confirmation.op, derr)
			}
		}
		if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: opPrefix + "-split"}); derr != nil {
			t.Fatalf("split: %v", derr)
		}
		if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: opPrefix + "-occupy"}); derr != nil {
			t.Fatalf("occupy: %v", derr)
		}
		for _, reading := range []struct {
			op  string
			day int32
		}{
			{op: "g2", day: 2},
			{op: "g5", day: 5},
			{op: "g8", day: 8},
		} {
			if _, derr := svc.RecordGermination(id, api.GerminationRequest{
				OperationID: opPrefix + "-" + reading.op,
				BlindCode:   code,
				DayAge:      reading.day,
				Normal:      95,
				Abnormal:    3,
				Dead:        2,
				Collector:   "germinator-c",
			}); derr != nil {
				t.Fatalf("germination day %d: %v", reading.day, derr)
			}
		}
		if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
			OperationID: opPrefix + "-p",
			BlindCode:   code,
			Plate:       create.Plate,
			Well:        "w1",
			Verifier:    "pathologist-d",
			Reading:     int32Ptr(10),
		}); derr != nil {
			t.Fatalf("pathogen: %v", derr)
		}
		if _, derr := svc.RecordMoisture(id, api.MoistureRequest{
			OperationID:   opPrefix + "-m",
			Moisture:      "12.50",
			PurityGrains:  98,
			TotalGrains:   100,
			ThousandGrain: 25000,
			Collector:     "metrologist-e",
		}); derr != nil {
			t.Fatalf("moisture: %v", derr)
		}
		for _, reviewReq := range []struct {
			op       string
			reviewer string
		}{
			{op: "r1", reviewer: "reviewer-f"},
			{op: "r2", reviewer: "reviewer-g"},
		} {
			if _, derr := svc.Review(id, api.ReviewRequest{
				OperationID: opPrefix + "-" + reviewReq.op,
				Reviewer:    reviewReq.reviewer,
				Conclusion:  "approve",
			}); derr != nil {
				t.Fatalf("review %s: %v", reviewReq.op, derr)
			}
		}
		return id
	}

	requireBlindSample := func(t *testing.T, svc *api.Service, id, code string, wantUnblinded bool) {
		t.Helper()

		view, derr := svc.GetTask(id)
		if derr != nil {
			t.Fatalf("get task: %v", derr)
		}
		for _, sample := range view.BlindSamples {
			if string(sample.Code) == code {
				if bool(sample.Unblinded) != wantUnblinded {
					t.Fatalf("blind code %s unblinded=%t, want %t", code, sample.Unblinded, wantUnblinded)
				}
				return
			}
		}
		t.Fatalf("blind code %s not found", code)
	}

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "opened code cannot release again from a later task",
			run: func(t *testing.T) {
				svc := seedService()

				firstID := driveToReleasableWithCode(t, svc, "opened-first", "lot-opened-1", "b1")
				firstFinal, derr := svc.Finalize(firstID, api.FinalizeRequest{OperationID: "opened-first-final"})
				if derr != nil {
					t.Fatalf("first finalize: %v", derr)
				}
				if firstFinal.Status != inspection.StatusReleased {
					t.Fatalf("first finalize status=%s, want %s", firstFinal.Status, inspection.StatusReleased)
				}
				if firstFinal.Credential == "" {
					t.Fatal("first release did not mint a credential")
				}
				requireBlindSample(t, svc, firstID, "b1", true)

				if _, derr := svc.Finalize(firstID, api.FinalizeRequest{OperationID: "opened-first-final-again"}); derr == nil {
					t.Fatal("repeat finalize on the same terminal task succeeded")
				} else if derr.Code != domain.CodeFinalized {
					t.Fatalf("repeat finalize code=%s, want %s", derr.Code, domain.CodeFinalized)
				}

				secondID := driveToReleasableWithCode(t, svc, "opened-second", "lot-opened-2", "b1")
				secondFinal, derr := svc.Finalize(secondID, api.FinalizeRequest{OperationID: "opened-second-final"})
				if derr == nil {
					t.Fatalf("second task reused opened blind code and released with credential %q", secondFinal.Credential)
				}
				if derr.Code != domain.CodeBlindDuplicate {
					t.Fatalf("second finalize code=%s, want %s", derr.Code, domain.CodeBlindDuplicate)
				}

				view, derr := svc.GetTask(secondID)
				if derr != nil {
					t.Fatalf("get second task: %v", derr)
				}
				if view.Task.Status != inspection.StatusReleasable {
					t.Fatalf("failed duplicate unblind changed second task status to %s", view.Task.Status)
				}
				if view.Credential != nil {
					t.Fatalf("failed duplicate unblind minted credential %q", view.Credential.Credential)
				}
				requireBlindSample(t, svc, secondID, "b1", false)
			},
		},
		{
			name: "cancelled unopened code can be recreated and released",
			run: func(t *testing.T) {
				svc := seedService()

				cancelledID := driveToReleasableWithCode(t, svc, "cancelled-first", "lot-cancelled-1", "b1")
				cancelledFinal, derr := svc.Finalize(cancelledID, api.FinalizeRequest{
					OperationID: "cancelled-first-final",
					Outcome:     "cancelled",
					Reason:      "operator stop",
				})
				if derr != nil {
					t.Fatalf("cancel finalize: %v", derr)
				}
				if cancelledFinal.Status != inspection.StatusCancelled {
					t.Fatalf("cancelled status=%s, want %s", cancelledFinal.Status, inspection.StatusCancelled)
				}
				if cancelledFinal.Credential != "" {
					t.Fatalf("cancelled task minted credential %q", cancelledFinal.Credential)
				}
				requireBlindSample(t, svc, cancelledID, "b1", false)

				reusedID := driveToReleasableWithCode(t, svc, "cancelled-reuse", "lot-cancelled-2", "b1")
				reusedFinal, derr := svc.Finalize(reusedID, api.FinalizeRequest{OperationID: "cancelled-reuse-final"})
				if derr != nil {
					t.Fatalf("reuse finalize: %v", derr)
				}
				if reusedFinal.Status != inspection.StatusReleased {
					t.Fatalf("reuse status=%s, want %s", reusedFinal.Status, inspection.StatusReleased)
				}
				if reusedFinal.Credential == "" {
					t.Fatal("reuse release did not mint a credential")
				}
				requireBlindSample(t, svc, reusedID, "b1", true)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

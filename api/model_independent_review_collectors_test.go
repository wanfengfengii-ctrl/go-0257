package api_test

import (
	"testing"

	"riceguard/api"
	"riceguard/catalog"
	"riceguard/domain"
	"riceguard/inspection"
	"riceguard/measure"
	"riceguard/pathogen"
	"riceguard/store"
)

func TestModel_IndependentReviewCollectorBoundary(t *testing.T) {
	type reviewAttempt struct {
		reviewer   string
		wantErr    bool
		wantStatus inspection.TaskStatus
	}

	cases := []struct {
		name                 string
		germinationCollector string
		pathogenVerifier     string
		moistureCollector    string
		attempts             []reviewAttempt
	}{
		{
			name:                 "two_uninvolved_reviewers_approve",
			germinationCollector: "germinator-c",
			pathogenVerifier:     "pathologist-d",
			moistureCollector:    "metrologist-e",
			attempts: []reviewAttempt{
				{reviewer: "reviewer-f", wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-g", wantStatus: inspection.StatusReleasable},
			},
		},
		{
			name:                 "duplicate_reviewer_rejected",
			germinationCollector: "germinator-c",
			pathogenVerifier:     "pathologist-d",
			moistureCollector:    "metrologist-e",
			attempts: []reviewAttempt{
				{reviewer: "reviewer-f", wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-f", wantErr: true, wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-g", wantStatus: inspection.StatusReleasable},
			},
		},
		{
			name:                 "non_reviewer_rejected",
			germinationCollector: "germinator-c",
			pathogenVerifier:     "pathologist-d",
			moistureCollector:    "metrologist-e",
			attempts: []reviewAttempt{
				{reviewer: "germinator-c", wantErr: true, wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-f", wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-g", wantStatus: inspection.StatusReleasable},
			},
		},
		{
			name:                 "germination_collector_with_reviewer_role_rejected",
			germinationCollector: "germination-reviewer",
			pathogenVerifier:     "pathologist-d",
			moistureCollector:    "metrologist-e",
			attempts: []reviewAttempt{
				{reviewer: "germination-reviewer", wantErr: true, wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-f", wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-g", wantStatus: inspection.StatusReleasable},
			},
		},
		{
			name:                 "pathogen_verifier_with_reviewer_role_rejected",
			germinationCollector: "germinator-c",
			pathogenVerifier:     "pathogen-reviewer",
			moistureCollector:    "metrologist-e",
			attempts: []reviewAttempt{
				{reviewer: "pathogen-reviewer", wantErr: true, wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-f", wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-g", wantStatus: inspection.StatusReleasable},
			},
		},
		{
			name:                 "moisture_collector_with_reviewer_role_rejected",
			germinationCollector: "germinator-c",
			pathogenVerifier:     "pathologist-d",
			moistureCollector:    "moisture-reviewer",
			attempts: []reviewAttempt{
				{reviewer: "moisture-reviewer", wantErr: true, wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-f", wantStatus: inspection.StatusPendingReview},
				{reviewer: "reviewer-g", wantStatus: inspection.StatusReleasable},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, roles := catalog.Seed()
			roles.Register(catalog.Personnel{
				ID:    "germination-reviewer",
				Roles: []catalog.RoleID{catalog.RoleGerminator, catalog.RoleReviewer},
			})
			roles.Register(catalog.Personnel{
				ID:    "pathogen-reviewer",
				Roles: []catalog.RoleID{catalog.RolePathologist, catalog.RoleReviewer},
			})
			roles.Register(catalog.Personnel{
				ID:    "moisture-reviewer",
				Roles: []catalog.RoleID{catalog.RoleMetrologist, catalog.RoleReviewer},
			})
			svc := api.NewService(c, roles, store.NewMemory(), pathogen.NewStaticAmplifier(), measure.NewScriptedMeter())
			op := "model-" + tc.name

			req := validCreate(op + "-create")
			req.ReviewerRoster = []string{"reviewer-f", "reviewer-g", "germination-reviewer", "pathogen-reviewer", "moisture-reviewer"}
			created, derr := svc.CreateTask(req)
			if derr != nil {
				t.Fatalf("create: %v", derr)
			}
			id := created.TaskID

			for i, reviewer := range []string{"sampler-a", "sampler-b"} {
				if _, derr := svc.ConfirmSampling(id, api.SamplingRequest{
					OperationID: op + "-sample-" + string(rune('a'+i)),
					Reviewer:    reviewer,
					Field:       "field-01",
					SeedLot:     "lot-1001",
					BlindSeal:   "seal-1",
					SampleCount: 180,
				}); derr != nil {
					t.Fatalf("sampling %s: %v", reviewer, derr)
				}
			}
			if _, derr := svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"}); derr != nil {
				t.Fatalf("split: %v", derr)
			}
			if _, derr := svc.Occupy(id, api.OccupyRequest{OperationID: op + "-occupy"}); derr != nil {
				t.Fatalf("occupy: %v", derr)
			}
			for _, day := range []int32{2, 5, 8} {
				if _, derr := svc.RecordGermination(id, api.GerminationRequest{
					OperationID: op + "-germination-" + string(rune('0'+day)),
					BlindCode:   "b1",
					DayAge:      day,
					Normal:      95,
					Abnormal:    3,
					Dead:        2,
					Collector:   tc.germinationCollector,
				}); derr != nil {
					t.Fatalf("germination day %d: %v", day, derr)
				}
			}
			if _, derr := svc.RecordPathogen(id, api.PathogenRequest{
				OperationID: op + "-pathogen",
				BlindCode:   "b1",
				Plate:       "p-1",
				Well:        "w1",
				Verifier:    tc.pathogenVerifier,
				Reading:     int32Ptr(10),
			}); derr != nil {
				t.Fatalf("pathogen: %v", derr)
			}
			if _, derr := svc.RecordMoisture(id, api.MoistureRequest{
				OperationID:   op + "-moisture",
				Moisture:      "12.50",
				PurityGrains:  98,
				TotalGrains:   100,
				ThousandGrain: 25000,
				Collector:     tc.moistureCollector,
			}); derr != nil {
				t.Fatalf("moisture: %v", derr)
			}

			acceptedReviews := 0
			for i, attempt := range tc.attempts {
				resp, derr := svc.Review(id, api.ReviewRequest{
					OperationID: op + "-review-" + string(rune('a'+i)),
					Reviewer:    attempt.reviewer,
					Conclusion:  "approve",
				})
				if attempt.wantErr {
					if derr == nil {
						t.Fatalf("review %s: expected rejection, got nil", attempt.reviewer)
					}
					if derr.Code != domain.CodeBadRequest {
						t.Fatalf("review %s: expected CodeBadRequest, got %s", attempt.reviewer, derr.Code)
					}
				} else {
					if derr != nil {
						t.Fatalf("review %s: %v", attempt.reviewer, derr)
					}
					acceptedReviews++
					if len(resp.Reviewers) != acceptedReviews {
						t.Fatalf("review %s: expected %d accepted reviews, got %d", attempt.reviewer, acceptedReviews, len(resp.Reviewers))
					}
				}

				view, derr := svc.GetTask(id)
				if derr != nil {
					t.Fatalf("view after %s: %v", attempt.reviewer, derr)
				}
				if view.Task.Status != attempt.wantStatus {
					t.Fatalf("review %s: expected status %s, got %s", attempt.reviewer, attempt.wantStatus, view.Task.Status)
				}
				if len(view.Reviews) != acceptedReviews {
					t.Fatalf("review %s: expected %d persisted reviews, got %d", attempt.reviewer, acceptedReviews, len(view.Reviews))
				}
			}
		})
	}
}

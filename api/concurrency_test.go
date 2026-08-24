package api_test

import (
	"sync"
	"testing"

	"riceguard/api"
	"riceguard/domain"
)

// TestConcurrentChamberOccupancyOneWinner drives two tasks competing for the
// same chamber window and asserts exactly one occupy succeeds.
func TestConcurrentChamberOccupancyOneWinner(t *testing.T) {
	svc := seedService()

	mk := func(op, lot, blind string) string {
		req := validCreate(op)
		req.SeedLot = lot
		req.BlindAllocs = []api.BlindAllocInput{{Code: blind, Germination: 100, Pathogen: 50, Moisture: 30}}
		resp, derr := svc.CreateTask(req)
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		id := resp.TaskID
		for i, r := range []string{"sampler-a", "sampler-b"} {
			svc.ConfirmSampling(id, api.SamplingRequest{OperationID: op + "-s" + string(rune('0'+i)), Reviewer: r, Field: "field-01", SeedLot: lot, BlindSeal: "seal-1", SampleCount: 180})
		}
		svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"})
		return id
	}

	idA := mk("cc-a", "lot-cc-a", "b-a")
	idB := mk("cc-b", "lot-cc-b", "b-b")

	var wg sync.WaitGroup
	results := make([]*domain.Error, 2)
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, results[0] = svc.Occupy(idA, api.OccupyRequest{OperationID: "cc-a-occupy"})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, results[1] = svc.Occupy(idB, api.OccupyRequest{OperationID: "cc-b-occupy"})
	}()
	close(start)
	wg.Wait()

	successes := 0
	conflicts := 0
	for _, r := range results {
		switch {
		case r == nil:
			successes++
		case r.Code == domain.CodeOccupancyConflict:
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected exactly one success and one conflict, got %d/%d", successes, conflicts)
	}
}

// TestConcurrentWellOccupancyOneWinner drives two tasks competing for the same
// plate well.
func TestConcurrentWellOccupancyOneWinner(t *testing.T) {
	svc := seedService()
	// Give each task its own chamber but the same plate/well.
	mk := func(op, lot, chamber, blind string) string {
		req := validCreate(op)
		req.SeedLot = lot
		req.Chamber = chamber
		req.BlindAllocs = []api.BlindAllocInput{{Code: blind, Germination: 100, Pathogen: 50, Moisture: 30}}
		resp, derr := svc.CreateTask(req)
		if derr != nil {
			t.Fatalf("create: %v", derr)
		}
		id := resp.TaskID
		for i, r := range []string{"sampler-a", "sampler-b"} {
			svc.ConfirmSampling(id, api.SamplingRequest{OperationID: op + "-s" + string(rune('0'+i)), Reviewer: r, Field: "field-01", SeedLot: lot, BlindSeal: "seal-1", SampleCount: 180})
		}
		svc.SplitBlindSamples(id, api.SplitRequest{OperationID: op + "-split"})
		return id
	}

	idA := mk("cw-a", "lot-cw-a", "ch-a", "b-a")
	idB := mk("cw-b", "lot-cw-b", "ch-b", "b-b")

	var wg sync.WaitGroup
	results := make([]*domain.Error, 2)
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, results[0] = svc.Occupy(idA, api.OccupyRequest{OperationID: "cw-a-occupy"})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, results[1] = svc.Occupy(idB, api.OccupyRequest{OperationID: "cw-b-occupy"})
	}()
	close(start)
	wg.Wait()

	successes := 0
	conflicts := 0
	for _, r := range results {
		switch {
		case r == nil:
			successes++
		case r.Code == domain.CodeOccupancyConflict:
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected exactly one success and one conflict, got %d/%d", successes, conflicts)
	}
}

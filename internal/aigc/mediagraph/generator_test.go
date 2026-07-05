package mediagraph

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

func TestGeneratorInterruptsForReferenceConfirmationAndResumes(t *testing.T) {
	ctx := context.Background()
	checkpoints := NewMemoryCheckpointStore()
	generator, err := NewGenerator(ctx, Config{Checkpoints: checkpoints})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	first, err := generator.Run(ctx, MediaGeneratorInput{
		SessionID:         "session-1",
		RunID:             "run-1",
		StoryboardID:      "storyboard-1",
		SpecVersion:       3,
		StoryboardVersion: 7,
		TargetType:        "all",
		MediaKinds:        []string{"image"},
	}, "graph-cp-1")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !first.Interrupted {
		t.Fatal("expected media graph to interrupt for reference confirmation")
	}
	if first.Interrupt == nil {
		t.Fatal("missing interrupt payload")
	}
	if first.Interrupt.Event != "a2ui.interrupt_request" {
		t.Fatalf("interrupt event = %q", first.Interrupt.Event)
	}
	payload := first.Interrupt.Payload
	if payload.CheckpointID != "graph-cp-1" || payload.InterruptID == "" {
		t.Fatalf("interrupt payload = %#v", payload)
	}
	if payload.SpecVersion != 3 || payload.StoryboardVersion != 7 {
		t.Fatalf("interrupt versions = %#v", payload)
	}
	if len(payload.Actions) == 0 || payload.Actions[0].Key != "confirm_reference_image" {
		t.Fatalf("interrupt actions = %#v", payload.Actions)
	}

	resumed, err := generator.Resume(ctx, "graph-cp-1", payload.InterruptID, ReferenceConfirmDecision{
		Approved: true,
		Note:     "参考图确认",
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Interrupted {
		t.Fatal("resume should complete the graph")
	}
	if resumed.Output.Status != StatusReferenceConfirmed {
		t.Fatalf("output status = %q", resumed.Output.Status)
	}
	if len(resumed.Output.Progress) < 6 {
		t.Fatalf("progress steps = %#v", resumed.Output.Progress)
	}
	if resumed.Output.StoryboardID != "storyboard-1" || resumed.Output.StoryboardVersion != 7 {
		t.Fatalf("output = %#v", resumed.Output)
	}
}

func TestGeneratorDispatchesJobsWhenDispatcherConfigured(t *testing.T) {
	ctx := context.Background()
	checkpoints := NewMemoryCheckpointStore()
	dispatcher := &fakeJobDispatcher{}
	generator, err := NewGenerator(ctx, Config{
		Checkpoints: checkpoints,
		Dispatcher:  dispatcher,
		NewID:       sequentialGraphIDs("job-image", "job-video"),
	})
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}

	first, err := generator.Run(ctx, MediaGeneratorInput{
		SessionID:         "session-1",
		StoryboardID:      "storyboard-1",
		StoryboardVersion: 7,
		TargetType:        generation.TargetKeyElement,
		TargetIDs:         []string{"suji"},
		MediaKinds:        []string{"image", "video"},
	}, "graph-cp-2")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !first.Interrupted {
		t.Fatal("expected interrupt after dispatching jobs")
	}
	if len(dispatcher.jobs) != 2 {
		t.Fatalf("jobs = %#v", dispatcher.jobs)
	}
	if dispatcher.jobs[0].ID != "job-image" || dispatcher.jobs[0].Provider != generation.ProviderImage2 {
		t.Fatalf("image job = %#v", dispatcher.jobs[0])
	}
	prompt, _ := dispatcher.jobs[0].Payload["prompt"].(string)
	filenamePrefix, _ := dispatcher.jobs[0].Payload["filename_prefix"].(string)
	stageKey, _ := dispatcher.jobs[0].Payload["stage_key"].(string)
	if prompt == "" || filenamePrefix == "" {
		t.Fatalf("image job missing prompt metadata = %#v", dispatcher.jobs[0].Payload)
	}
	if stageKey != "generate_key_element_image" {
		t.Fatalf("image job stage key = %q", stageKey)
	}
	if dispatcher.jobs[1].ID != "job-video" || dispatcher.jobs[1].Provider != generation.ProviderSeedance {
		t.Fatalf("video job = %#v", dispatcher.jobs[1])
	}

	resumed, err := generator.Resume(ctx, "graph-cp-2", first.Interrupt.Payload.InterruptID, ReferenceConfirmDecision{Approved: true})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if len(resumed.Output.JobIDs) != 2 || resumed.Output.JobIDs[0] != "job-image" || resumed.Output.JobIDs[1] != "job-video" {
		t.Fatalf("output job ids = %#v", resumed.Output.JobIDs)
	}
}

type fakeJobDispatcher struct {
	jobs []generation.GenerationJob
}

func (d *fakeJobDispatcher) Dispatch(_ context.Context, job generation.GenerationJob) (generation.GenerationJob, bool, error) {
	d.jobs = append(d.jobs, job)
	return job, true, nil
}

func sequentialGraphIDs(ids ...string) func() string {
	i := 0
	return func() string {
		if i >= len(ids) {
			return ids[len(ids)-1]
		}
		id := ids[i]
		i++
		return id
	}
}

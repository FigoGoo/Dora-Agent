package mediapreview

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeEnvelopeV1RejectsUnknownFieldAndProfileMismatch(t *testing.T) {
	t.Parallel()
	envelope := validGenerateEnvelope(t, "staging/objects/output.png")
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	decoded, err := DecodeEnvelopeV1(payload)
	if err != nil {
		t.Fatalf("decode valid envelope: %v", err)
	}
	if decoded.JobID != envelope.JobID || decoded.JobType != JobTypeGeneratePNG {
		t.Fatalf("decoded envelope mismatch: %+v", decoded)
	}

	unknownPayload := strings.Replace(string(payload), `"schema_version"`, `"unexpected":true,"schema_version"`, 1)
	if _, err := DecodeEnvelopeV1([]byte(unknownPayload)); err == nil || CodeOf(err) != ErrorCodeInvalidArgument {
		t.Fatalf("unknown field should be rejected, got %v", err)
	}

	envelope.OutputProfile = OutputProfileMP4H264640x3602sV1
	if err := envelope.Validate(); err == nil || CodeOf(err) != ErrorCodeInvalidArgument {
		t.Fatalf("mismatched JobType/Profile should be rejected, got %v", err)
	}
}

func TestDecodeEnvelopeV1RejectsNestedUnknownField(t *testing.T) {
	t.Parallel()
	envelope := validGenerateEnvelope(t, "staging/objects/output.png")
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	unknownPayload := strings.Replace(string(payload), `"source_type"`, `"metadata":{},"source_type"`, 1)
	if _, err := DecodeEnvelopeV1([]byte(unknownPayload)); err == nil || CodeOf(err) != ErrorCodeInvalidArgument {
		t.Fatalf("nested unknown field should be rejected, got %v", err)
	}
}

func TestAssembleEnvelopeRejectsSourceTargetAlias(t *testing.T) {
	t.Parallel()
	envelope := validAssembleEnvelope(t, "objects/source.png", "objects/source.png")
	if err := envelope.Validate(); err == nil || CodeOf(err) != ErrorCodeInvalidArgument {
		t.Fatalf("source and target object key alias should be rejected, got %v", err)
	}
}

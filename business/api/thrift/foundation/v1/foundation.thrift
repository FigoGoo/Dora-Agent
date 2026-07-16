namespace go foundationv1

const string FOUNDATION_SCHEMA_VERSION = "foundation.rpc.v1"
const string BUSINESS_FOUNDATION_SERVICE_NAME = "dora.business.foundation.v1"

struct FoundationProbeRequestV1 {
    1: required string schema_version
    2: required string request_id
    3: required string caller_service
    4: required string caller_version
    5: required i64 sent_at_unix_ms
}

struct FoundationProbeResponseV1 {
    1: required string schema_version
    2: required string request_id
    3: required string service_name
    4: required string service_version
    5: required string environment
    6: required string instance_id
    7: required i64 received_at_unix_ms
}

exception FoundationServiceExceptionV1 {
    1: required string code
    2: required string message
    3: required bool retryable
}

service BusinessFoundationServiceV1 {
    FoundationProbeResponseV1 Probe(1: FoundationProbeRequestV1 request)
        throws (1: FoundationServiceExceptionV1 service_error)
}

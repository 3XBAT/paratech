package shared_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	_ "google.golang.org/protobuf/types/known/anypb"    // registers google/protobuf/any.proto for the synthetic descriptor
	_ "google.golang.org/protobuf/types/known/structpb" // registers google/protobuf/struct.proto for the synthetic descriptor

	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

const hubPackage = "paratech.hub.v1"

// Words that must not appear as a part of any field name of the Hub contract. A client must never choose where the Hub
// connects (PRD section 13.2, hard rules), and credentials travel in a header, not in message bodies (PRD section 12).
// Names are matched per snake_case word, so "duration" or "security" are fine.
var (
	forbiddenWords = []string{
		"url", "urls", "uri", "uris", "endpoint", "endpoints", "host", "hosts", "hostname", "baseurl",
		"webhook", "webhooks", "proxy", "proxies", "link", "links", "address", "addresses", "destination", "destinations",
		"secret", "password", "bearer", "authorization", "credential", "credentials", "apikey",
	}
	// Forbidden as a consecutive pair of words.
	forbiddenPairs = [][2]string{{"api", "key"}, {"base", "url"}, {"access", "token"}}
	// Untyped containers can smuggle any field (a URL included) past this test, so a message may not use them.
	forbiddenTypes = []protoreflect.FullName{
		"google.protobuf.Any", "google.protobuf.Struct", "google.protobuf.Value", "google.protobuf.ListValue",
	}
)

// walkFields calls fn for every field of every message (nested messages included).
func walkFields(files []protoreflect.FileDescriptor, fn func(msg protoreflect.MessageDescriptor, f protoreflect.FieldDescriptor)) {
	var walk func(protoreflect.MessageDescriptors)
	walk = func(msgs protoreflect.MessageDescriptors) {
		for i := range msgs.Len() {
			m := msgs.Get(i)
			for j := range m.Fields().Len() {
				fn(m, m.Fields().Get(j))
			}
			walk(m.Messages())
		}
	}
	for _, f := range files {
		walk(f.Messages())
	}
}

// forbiddenFieldNames returns "Message.field" for every field whose name contains a forbidden word, for every float or
// double field (quota amounts and token counts are exact integers), and for every field of an untyped container type
// (Any, Struct, Value, ListValue).
func forbiddenFieldNames(files ...protoreflect.FileDescriptor) []string {
	var found []string
	walkFields(files, func(m protoreflect.MessageDescriptor, f protoreflect.FieldDescriptor) {
		bad := badName(string(f.Name())) || f.Kind() == protoreflect.FloatKind || f.Kind() == protoreflect.DoubleKind
		if f.Message() != nil && slices.Contains(forbiddenTypes, f.Message().FullName()) {
			bad = true
		}
		if bad {
			found = append(found, string(m.FullName())+"."+string(f.Name()))
		}
	})
	return found
}

func badName(name string) bool {
	words := strings.Split(strings.ToLower(name), "_")
	for _, w := range words {
		if slices.Contains(forbiddenWords, w) {
			return true
		}
	}
	for i := 0; i+1 < len(words); i++ {
		if slices.Contains(forbiddenPairs, [2]string{words[i], words[i+1]}) {
			return true
		}
	}
	return false
}

func hubFiles(t *testing.T) []protoreflect.FileDescriptor {
	t.Helper()
	var files []protoreflect.FileDescriptor
	protoregistry.GlobalFiles.RangeFilesByPackage(hubPackage, func(fd protoreflect.FileDescriptor) bool {
		files = append(files, fd)
		return true
	})
	if len(files) < 4 {
		t.Fatalf("package %s has %d files registered, want at least 4 (common, errors, gateway, quota)", hubPackage, len(files))
	}
	return files
}

func TestHubMessagesHaveNoClientChosenDestinationOrSecretFields(t *testing.T) {
	if bad := forbiddenFieldNames(hubFiles(t)...); len(bad) > 0 {
		t.Fatalf("fields that let a client choose a destination, carry a secret, use floating point or an untyped container: %v", bad)
	}
}

// The checker itself must be able to fail, otherwise the test above proves nothing.
func TestForbiddenFieldCheckerCatchesBadFields(t *testing.T) {
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	dbl := descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum()
	msg := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
	field := func(name string, n int32, typ *descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name: proto.String(name), Number: proto.Int32(n), Type: typ,
			Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		}
	}
	typed := func(name string, n int32, typeName string) *descriptorpb.FieldDescriptorProto {
		f := field(name, n, msg)
		f.TypeName = proto.String(typeName)
		return f
	}
	fdp := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("synthetic.proto"),
		Package:    proto.String("synthetic.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/any.proto", "google/protobuf/struct.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Bad"),
			Field: []*descriptorpb.FieldDescriptorProto{
				field("base_url", 1, str),
				field("callback_uri", 2, str),
				field("provider_host", 3, str),
				field("Endpoint", 4, str),
				field("api_key", 5, str),
				field("price", 6, dbl),
				field("webhook_target", 10, str),
				field("egress_proxy", 11, str),
				field("download_link", 12, str),
				field("mail_address", 13, str),
				field("destination", 14, str),
				typed("extra", 15, ".google.protobuf.Any"),
				typed("options", 16, ".google.protobuf.Struct"),
				// Look-alikes that are fine: the word is only a substring.
				field("duration", 7, str),
				field("security_level", 8, str),
				field("ghost", 9, str),
				field("linkage", 17, str),
				field("addressee", 18, str),
			},
			NestedType: []*descriptorpb.DescriptorProto{{
				Name:  proto.String("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{field("upstream_url", 1, str)},
			}},
		}},
	}
	fd, err := protodesc.NewFile(fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("build synthetic descriptor: %v", err)
	}
	got := forbiddenFieldNames(fd)
	want := []string{
		"synthetic.v1.Bad.base_url", "synthetic.v1.Bad.callback_uri", "synthetic.v1.Bad.provider_host",
		"synthetic.v1.Bad.Endpoint", "synthetic.v1.Bad.api_key", "synthetic.v1.Bad.price", "synthetic.v1.Bad.Inner.upstream_url",
		"synthetic.v1.Bad.webhook_target", "synthetic.v1.Bad.egress_proxy", "synthetic.v1.Bad.download_link",
		"synthetic.v1.Bad.mail_address", "synthetic.v1.Bad.destination", "synthetic.v1.Bad.extra", "synthetic.v1.Bad.options",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("checker found %v, want %v", got, want)
	}
}

// fieldType describes the wire-relevant type of a field in a stable text form.
func fieldType(f protoreflect.FieldDescriptor) string {
	var typ string
	switch f.Kind() {
	case protoreflect.EnumKind:
		typ = "enum " + string(f.Enum().FullName())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		typ = "message " + string(f.Message().FullName())
	default:
		typ = f.Kind().String()
	}
	if f.IsList() {
		typ = "repeated " + typ
	}
	if oneof := f.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
		typ += " (oneof " + string(oneof.Name()) + ")"
	}
	return typ
}

// The full shape of every message. Adding, removing, renaming or retyping a field is a contract change and must be a
// conscious edit of this table (buf breaking guards the compatibility, this guards the intent and the field types).
var wantFields = map[string]map[string]string{
	"QuotaAmount": {"unit": "enum paratech.hub.v1.QuotaUnit", "value": "int64"},
	"QuotaSnapshot": {
		"remaining": "message paratech.hub.v1.QuotaAmount", "reserved": "message paratech.hub.v1.QuotaAmount",
		"limit": "message paratech.hub.v1.QuotaAmount", "period_ends_at": "message google.protobuf.Timestamp",
	},
	"Usage": {"input_tokens": "int64", "output_tokens": "int64"},
	"ErrorDetail": {
		"code": "enum paratech.hub.v1.ErrorCode", "retry_after": "message google.protobuf.Duration", "message": "string",
		"quota": "message paratech.hub.v1.QuotaSnapshot", "retryable": "bool",
	},
	"CompletionSpec": {
		"idempotency_key": "string", "operation_type": "enum paratech.hub.v1.OperationType", "model_alias": "string",
		"instructions": "string", "untrusted_data": "string", "max_output_tokens": "uint32",
	},
	"CompletionResult": {
		"output": "string", "usage": "message paratech.hub.v1.Usage", "quota": "message paratech.hub.v1.QuotaSnapshot",
		"reservation_id": "string", "replayed": "bool",
	},
	"CompleteRequest":        {"spec": "message paratech.hub.v1.CompletionSpec"},
	"CompleteResponse":       {"result": "message paratech.hub.v1.CompletionResult"},
	"CompleteStreamRequest":  {"spec": "message paratech.hub.v1.CompletionSpec"},
	"CompleteStreamResponse": {"delta": "string (oneof event)", "result": "message paratech.hub.v1.CompletionResult (oneof event)"},
	"Reservation": {
		"reservation_id": "string", "state": "enum paratech.hub.v1.ReservationState",
		"reserved": "message paratech.hub.v1.QuotaAmount", "charged": "message paratech.hub.v1.QuotaAmount",
		"expires_at": "message google.protobuf.Timestamp",
	},
	"ReserveRequest": {
		"idempotency_key": "string", "operation_type": "enum paratech.hub.v1.OperationType", "model_alias": "string",
		"amount": "message paratech.hub.v1.QuotaAmount",
	},
	"ReserveResponse":      {"reservation": "message paratech.hub.v1.Reservation", "quota": "message paratech.hub.v1.QuotaSnapshot"},
	"SettleRequest":        {"reservation_id": "string", "actual": "message paratech.hub.v1.QuotaAmount", "partial": "bool"},
	"SettleResponse":       {"reservation": "message paratech.hub.v1.Reservation", "quota": "message paratech.hub.v1.QuotaSnapshot"},
	"ReleaseRequest":       {"reservation_id": "string"},
	"ReleaseResponse":      {"reservation": "message paratech.hub.v1.Reservation", "quota": "message paratech.hub.v1.QuotaSnapshot"},
	"GetRemainingRequest":  {},
	"GetRemainingResponse": {"quota": "message paratech.hub.v1.QuotaSnapshot"},
	"AgentStepRequest": {
		"idempotency_key": "string", "thread_id": "string", "profile": "enum paratech.hub.v1.AgentProfile",
		"catalog_version": "uint32", "base": "message paratech.hub.v1.AgentBase", "scope": "message paratech.hub.v1.AgentScope",
		"turns": "repeated message paratech.hub.v1.AgentTurn", "max_output_tokens": "uint32",
	},
	"AgentBase":    {"subjects": "repeated message paratech.hub.v1.AgentSubject", "today": "string", "time_zone": "string"},
	"AgentSubject": {"subject_id": "string", "name": "string"},
	"AgentScope": {
		"subject_id": "string", "screen": "enum paratech.hub.v1.AgentScreen", "lesson_id": "string", "assignment_id": "string",
	},
	"AgentTurn": {
		"student_text": "string (oneof turn)", "calls": "message paratech.hub.v1.SealedCalls (oneof turn)",
		"results": "message paratech.hub.v1.ToolResults (oneof turn)",
	},
	"SealedCalls": {"calls": "repeated message paratech.hub.v1.ToolCall", "seal": "bytes"},
	"ToolCall":    {"call_id": "string", "tool": "string", "arguments_json": "string"},
	"ToolResults": {"results": "repeated message paratech.hub.v1.ToolResult"},
	"ToolResult":  {"call_id": "string", "result_json": "string", "is_error": "bool"},
	"AgentStepResponse": {
		"calls": "message paratech.hub.v1.SealedCalls", "usage": "message paratech.hub.v1.Usage",
		"quota": "message paratech.hub.v1.QuotaSnapshot", "reservation_id": "string", "replayed": "bool",
	},
}

// Every string and bytes field of the contract. These carry free text, so a new one is a new place where a destination,
// a secret or transcript-derived text could travel: adding one must be a conscious edit here, with a review of what the
// Hub does with it. Sorted by field name.
var wantStringFields = map[string][]string{
	"CompletionSpec":         {"idempotency_key", "instructions", "model_alias", "untrusted_data"},
	"CompletionResult":       {"output", "reservation_id"},
	"CompleteStreamResponse": {"delta"},
	"ErrorDetail":            {"message"},
	"Reservation":            {"reservation_id"},
	"ReserveRequest":         {"idempotency_key", "model_alias"},
	"SettleRequest":          {"reservation_id"},
	"ReleaseRequest":         {"reservation_id"},
	"AgentStepRequest":       {"idempotency_key", "thread_id"},
	"AgentBase":              {"time_zone", "today"},
	"AgentSubject":           {"name", "subject_id"},
	"AgentScope":             {"assignment_id", "lesson_id", "subject_id"},
	"AgentTurn":              {"student_text"},
	"SealedCalls":            {"seal"},
	"ToolCall":               {"arguments_json", "call_id", "tool"},
	"ToolResult":             {"call_id", "result_json"},
	"AgentStepResponse":      {"reservation_id"},
}

func localName(m protoreflect.MessageDescriptor) string {
	return strings.TrimPrefix(string(m.FullName()), hubPackage+".")
}

func TestMessageFieldsAndTypesAreAsDeclared(t *testing.T) {
	got := map[string]map[string]string{}
	gotStrings := map[string][]string{}
	walkFields(hubFiles(t), func(m protoreflect.MessageDescriptor, f protoreflect.FieldDescriptor) {
		name := localName(m)
		if got[name] == nil {
			got[name] = map[string]string{}
		}
		got[name][string(f.Name())] = fieldType(f)
		if f.Kind() == protoreflect.StringKind || f.Kind() == protoreflect.BytesKind {
			gotStrings[name] = append(gotStrings[name], string(f.Name()))
		}
	})
	// Messages without fields never reach the callback; register them from the descriptors.
	for _, fd := range hubFiles(t) {
		for i := range fd.Messages().Len() {
			if name := localName(fd.Messages().Get(i)); got[name] == nil {
				got[name] = map[string]string{}
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(got)) {
		want, ok := wantFields[name]
		if !ok {
			t.Errorf("message %s is not in wantFields: a new message is a contract change, add it consciously", name)
			continue
		}
		for _, field := range slices.Sorted(maps.Keys(got[name])) {
			if want[field] != got[name][field] {
				t.Errorf("%s.%s is %q, the contract table says %q", name, field, got[name][field], want[field])
			}
		}
		for field := range want {
			if _, ok := got[name][field]; !ok {
				t.Errorf("%s.%s is in the contract table but missing from the proto", name, field)
			}
		}
	}
	for name := range wantFields {
		if _, ok := got[name]; !ok {
			t.Errorf("message %s is in the contract table but missing from the proto", name)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(gotStrings)) {
		slices.Sort(gotStrings[name])
		if !slices.Equal(gotStrings[name], wantStringFields[name]) {
			t.Errorf("string/bytes fields of %s are %v, the golden list says %v: a new free-text field must be a conscious edit",
				name, gotStrings[name], wantStringFields[name])
		}
	}
	for name, fields := range wantStringFields {
		if _, ok := gotStrings[name]; !ok {
			t.Errorf("%s has no string/bytes fields any more, but the golden list names %v", name, fields)
		}
	}
}

func TestErrorCodesExist(t *testing.T) {
	values := hubv1.ErrorCode(0).Descriptor().Values()
	if values.Len() < 5 {
		t.Fatalf("ErrorCode has %d values, want UNSPECIFIED plus at least the four typed codes", values.Len())
	}
	if first := values.ByNumber(0); first == nil || first.Name() != "ERROR_CODE_UNSPECIFIED" {
		t.Fatalf("ErrorCode zero value must be ERROR_CODE_UNSPECIFIED, got %v", first)
	}
	for _, name := range []string{"QUOTA_EXCEEDED", "RATE_LIMITED", "INVALID_API_KEY", "PROVIDER_ERROR"} {
		v := values.ByName(protoreflect.Name("ERROR_CODE_" + name))
		if v == nil {
			t.Errorf("ErrorCode %s is missing", name)
		} else if v.Number() == 0 {
			t.Errorf("ErrorCode %s must not be the zero value", name)
		}
	}
}

// Exposure of the RPCs. "public" is served on the Hub's API-key-protected listener; "internal" only to the Hub's own
// gateway path (see the exposure note in quota.proto). Exposure is not part of the descriptor, so this table is its only
// machine-readable home: moving an RPC between the two, or adding one, must be a conscious edit of it.
const (
	public   = "public"
	internal = "internal"
)

type rpcShape struct {
	request, response string
	serverStream      bool
	exposure          string
}

var wantRPCs = map[string]rpcShape{
	"GatewayService.Complete":       {"CompleteRequest", "CompleteResponse", false, public},
	"GatewayService.CompleteStream": {"CompleteStreamRequest", "CompleteStreamResponse", true, public},
	"GatewayService.AgentStep":      {"AgentStepRequest", "AgentStepResponse", false, public},
	"QuotaService.Reserve":          {"ReserveRequest", "ReserveResponse", false, internal},
	"QuotaService.Settle":           {"SettleRequest", "SettleResponse", false, internal},
	"QuotaService.Release":          {"ReleaseRequest", "ReleaseResponse", false, internal},
	"QuotaService.GetRemaining":     {"GetRemainingRequest", "GetRemainingResponse", false, public},
}

func TestServicesRPCsAndExposure(t *testing.T) {
	got := map[string]rpcShape{}
	for _, fd := range hubFiles(t) {
		for i := range fd.Services().Len() {
			svc := fd.Services().Get(i)
			for j := range svc.Methods().Len() {
				m := svc.Methods().Get(j)
				key := string(svc.Name()) + "." + string(m.Name())
				if m.IsStreamingClient() {
					t.Errorf("%s: client streaming is not part of the contract", key)
				}
				got[key] = rpcShape{
					request: localName(m.Input()), response: localName(m.Output()), serverStream: m.IsStreamingServer(),
					exposure: wantRPCs[key].exposure, // not in the descriptor, see wantRPCs
				}
			}
		}
	}
	for key, want := range wantRPCs {
		g, ok := got[key]
		if !ok {
			t.Errorf("%s is in the RPC table but missing from the proto", key)
			continue
		}
		if g != want {
			t.Errorf("%s is %+v, the RPC table says %+v", key, g, want)
		}
	}
	for key := range got {
		if _, ok := wantRPCs[key]; !ok {
			t.Errorf("%s is a new RPC: add it to wantRPCs and decide whether it is public or internal", key)
		}
	}
}

// Reserve, Settle and Release take quota decisions (Settle even takes provider-reported usage) and must never be
// registered on the public listener; the gateway and GetRemaining are the only public surface.
func TestOnlyGatewayAndGetRemainingArePublic(t *testing.T) {
	var publicRPCs, internalRPCs []string
	for key, r := range wantRPCs {
		switch r.exposure {
		case public:
			publicRPCs = append(publicRPCs, key)
		case internal:
			internalRPCs = append(internalRPCs, key)
		default:
			t.Errorf("%s has exposure %q, want %q or %q", key, r.exposure, public, internal)
		}
	}
	slices.Sort(publicRPCs)
	slices.Sort(internalRPCs)
	if want := []string{
		"GatewayService.AgentStep", "GatewayService.Complete", "GatewayService.CompleteStream", "QuotaService.GetRemaining",
	}; !slices.Equal(publicRPCs, want) {
		t.Errorf("public RPCs are %v, want %v", publicRPCs, want)
	}
	if want := []string{"QuotaService.Release", "QuotaService.Reserve", "QuotaService.Settle"}; !slices.Equal(internalRPCs, want) {
		t.Errorf("internal RPCs are %v, want %v", internalRPCs, want)
	}
}

func TestEnumsOfTheContract(t *testing.T) {
	want := map[string][]string{
		"OperationType": {
			"OPERATION_TYPE_UNSPECIFIED", "OPERATION_TYPE_SUMMARY", "OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES", "OPERATION_TYPE_HOMEWORK_DRAFT",
			"OPERATION_TYPE_LECTURE_TEXT", "OPERATION_TYPE_AGENT_STEP", "OPERATION_TYPE_INSPECT_MATERIAL", "OPERATION_TYPE_ANSWER",
			"OPERATION_TYPE_TRIAGE_ASSIGNMENT",
		},
		"AgentProfile": {"AGENT_PROFILE_UNSPECIFIED", "AGENT_PROFILE_PANEL"},
		"AgentScreen": {
			"AGENT_SCREEN_UNSPECIFIED", "AGENT_SCREEN_TODAY", "AGENT_SCREEN_SUBJECTS", "AGENT_SCREEN_ASSIGNMENTS",
			"AGENT_SCREEN_SCHEDULE", "AGENT_SCREEN_JOURNAL", "AGENT_SCREEN_SETTINGS",
		},
		"QuotaUnit": {"QUOTA_UNIT_UNSPECIFIED", "QUOTA_UNIT_TOKENS"},
		"ReservationState": {
			"RESERVATION_STATE_UNSPECIFIED", "RESERVATION_STATE_RESERVED", "RESERVATION_STATE_SETTLED", "RESERVATION_STATE_RELEASED",
		},
	}
	for name, values := range want {
		d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(hubPackage + "." + name))
		if err != nil {
			t.Fatalf("enum %s: %v", name, err)
		}
		var got []string
		vs := d.(protoreflect.EnumDescriptor).Values()
		for i := range vs.Len() {
			if vs.Get(i).Number() != protoreflect.EnumNumber(i) {
				t.Errorf("%s value %s has number %d, want %d", name, vs.Get(i).Name(), vs.Get(i).Number(), i)
			}
			got = append(got, string(vs.Get(i).Name()))
		}
		if !slices.Equal(got, values) {
			t.Errorf("enum %s is %v, want %v", name, got, values)
		}
	}
}

// Agent messages (ADR-0022): on top of forbiddenWords, no field or type of an agent message may name a recipient, a
// path or a file. Files are addressed by ID and the recipient comes from settings, never from model text.
var agentForbiddenWords = []string{
	"email", "emails", "mail", "mails", "mailbox", "recipient", "recipients", "path", "paths", "filepath",
	"file", "files", "filename", "filenames", "dir", "dirs", "directory", "directories",
}

// Every string and bytes field reachable from AgentStepRequest and AgentStepResponse, with what it carries. A new
// free-text field fails TestAgentMessagesHaveOnlyAllowlistedStrings until it is reviewed and added here.
var agentStringFields = map[string]string{
	"AgentStepRequest.idempotency_key": "client key, printable ASCII",
	"AgentStepRequest.thread_id":       "opaque desktop ID",
	"AgentBase.today":                  "date YYYY-MM-DD",
	"AgentBase.time_zone":              "IANA zone name",
	"AgentSubject.subject_id":          "opaque desktop ID",
	"AgentSubject.name":                "subject name, label of at most 80 characters",
	"AgentScope.subject_id":            "opaque desktop ID",
	"AgentScope.lesson_id":             "opaque desktop ID",
	"AgentScope.assignment_id":         "opaque desktop ID",
	"AgentTurn.student_text":           "what the student typed, at most 4 KiB",
	"SealedCalls.seal":                 "HMAC with a key version byte",
	"ToolCall.call_id":                 "opaque call ID",
	"ToolCall.tool":                    "tool name of the catalog",
	"ToolCall.arguments_json":          "JSON object of IDs, enums, dates and labels",
	"ToolResult.call_id":               "opaque call ID",
	"ToolResult.result_json":           "JSON built from an allowlist of typed fields",
	"AgentStepResponse.reservation_id": "quota reservation ID",
}

// agentMessages returns the messages of the Hub package reachable from the agent RPC, roots included.
func agentMessages(t *testing.T) []protoreflect.MessageDescriptor {
	t.Helper()
	var out []protoreflect.MessageDescriptor
	seen := map[protoreflect.FullName]bool{}
	var visit func(m protoreflect.MessageDescriptor)
	visit = func(m protoreflect.MessageDescriptor) {
		if seen[m.FullName()] || m.ParentFile().Package() != hubPackage {
			return
		}
		seen[m.FullName()] = true
		out = append(out, m)
		for i := range m.Fields().Len() {
			if sub := m.Fields().Get(i).Message(); sub != nil {
				visit(sub)
			}
		}
	}
	visit((&hubv1.AgentStepRequest{}).ProtoReflect().Descriptor())
	visit((&hubv1.AgentStepResponse{}).ProtoReflect().Descriptor())
	if len(out) < 10 {
		t.Fatalf("only %d agent messages reachable, the walk is broken", len(out))
	}
	return out
}

// camelWords splits "MailAddress" into "mail", "address".
func camelWords(name string) []string {
	var words []string
	start := 0
	for i := 1; i < len(name); i++ {
		if name[i] >= 'A' && name[i] <= 'Z' {
			words = append(words, strings.ToLower(name[start:i]))
			start = i
		}
	}
	return append(words, strings.ToLower(name[start:]))
}

func badAgentWords(words []string) bool {
	for _, w := range words {
		if slices.Contains(agentForbiddenWords, w) {
			return true
		}
	}
	return false
}

// agentForbiddenFields returns "Message.field" for every field whose name, or whose message or enum type name, names a
// destination, a secret, a recipient, a path or a file, and for every untyped container.
func agentForbiddenFields(msgs []protoreflect.MessageDescriptor) []string {
	var found []string
	for _, m := range msgs {
		for i := range m.Fields().Len() {
			f := m.Fields().Get(i)
			name := string(f.Name())
			bad := badName(name) || badAgentWords(strings.Split(strings.ToLower(name), "_"))
			var typeName protoreflect.Name
			switch {
			case f.Message() != nil:
				typeName = f.Message().Name()
				bad = bad || slices.Contains(forbiddenTypes, f.Message().FullName())
			case f.Enum() != nil:
				typeName = f.Enum().Name()
			}
			if typeName != "" {
				words := camelWords(string(typeName))
				bad = bad || badAgentWords(words) || badName(strings.Join(words, "_"))
			}
			if bad {
				found = append(found, string(m.FullName())+"."+name)
			}
		}
	}
	return found
}

func TestAgentMessagesHaveNoAddressPathOrFileFields(t *testing.T) {
	if bad := agentForbiddenFields(agentMessages(t)); len(bad) > 0 {
		t.Fatalf("agent fields that name a destination, a recipient, a path or a file (ADR-0022): %v", bad)
	}
}

// The agent checker must be able to fail, otherwise the test above proves nothing.
func TestAgentFieldCheckerCatchesBadFields(t *testing.T) {
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	field := func(name string, n int32) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name: proto.String(name), Number: proto.Int32(n), Type: str,
			Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		}
	}
	typed := func(name string, n int32, typeName string) *descriptorpb.FieldDescriptorProto {
		f := field(name, n)
		f.Type = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
		f.TypeName = proto.String(typeName)
		return f
	}
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("synthetic_agent.proto"),
		Package: proto.String("synthetic.agent.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("MailAddress"), Field: []*descriptorpb.FieldDescriptorProto{field("value", 1)}},
			{Name: proto.String("SourceUrl"), Field: []*descriptorpb.FieldDescriptorProto{field("value", 1)}},
			{
				Name: proto.String("Call"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("recipient_email", 1),
					field("save_path", 2),
					field("file_name", 3),
					field("target_dir", 4),
					field("callback_url", 5),
					typed("to", 6, ".synthetic.agent.v1.MailAddress"),
					typed("source", 7, ".synthetic.agent.v1.SourceUrl"),
					// Look-alikes that are fine: the word is only a substring.
					field("profile", 8),
					field("filed_at", 9),
					field("pathway", 10),
				},
			},
		},
	}
	fd, err := protodesc.NewFile(fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("build synthetic descriptor: %v", err)
	}
	got := agentForbiddenFields([]protoreflect.MessageDescriptor{fd.Messages().ByName("Call")})
	want := []string{
		"synthetic.agent.v1.Call.recipient_email", "synthetic.agent.v1.Call.save_path", "synthetic.agent.v1.Call.file_name",
		"synthetic.agent.v1.Call.target_dir", "synthetic.agent.v1.Call.callback_url", "synthetic.agent.v1.Call.to",
		"synthetic.agent.v1.Call.source",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("checker found %v, want %v", got, want)
	}
}

func TestAgentMessagesHaveOnlyAllowlistedStrings(t *testing.T) {
	got := map[string]bool{}
	for _, m := range agentMessages(t) {
		for i := range m.Fields().Len() {
			f := m.Fields().Get(i)
			if f.Kind() != protoreflect.StringKind && f.Kind() != protoreflect.BytesKind {
				continue
			}
			key := localName(m) + "." + string(f.Name())
			got[key] = true
			if _, ok := agentStringFields[key]; !ok {
				t.Errorf("%s is a string/bytes field of an agent message that is not in agentStringFields: review it first", key)
			}
		}
	}
	for key := range agentStringFields {
		if !got[key] {
			t.Errorf("%s is in agentStringFields but not a string/bytes field of an agent message", key)
		}
	}
}

// Golden tool catalog v1 (design §2.3, ADR-0021). Both the Hub catalog (TASK-114) and the desktop Tool enum (TASK-115)
// must match it.
const toolCatalogFile = "testdata/tool_catalog_v1.json"

type catalogArgument struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Values   []string `json:"values"`
}

type catalogTool struct {
	Name            string            `json:"name"`
	Class           string            `json:"class"`
	Paid            bool              `json:"paid"`
	AgentCanExecute bool              `json:"agent_can_execute"`
	Arguments       []catalogArgument `json:"arguments"`
}

type toolCatalog struct {
	CatalogVersion uint32            `json:"catalog_version"`
	Types          map[string]string `json:"types"`
	Tools          []catalogTool     `json:"tools"`
}

var (
	toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	toolClasses     = []string{"R", "U", "C", "UI"}
	// Argument types of tools that act or read: only IDs, enums, dates and short labels (ADR-0022).
	actionArgTypes = []string{"id", "ids", "date", "enum", "label"}
	// Types only UI tools may use: card text written by the model, at most 300 characters each (ADR-0023).
	uiArgTypes = []string{"text", "text_list", "plan_steps", "questions"}
	// Class C tools only name trusted local objects: the confirm card shows trusted data, never model text.
	confirmArgTypes = []string{"id"}
	uiTools         = []string{"ask_questions", "cannot_do", "reply", "show_plan"}
	// Argument names that would let a model choose a recipient, a destination or a file outside the app.
	forbiddenArgWords = []string{"cc", "bcc", "subject_line", "attachment", "attachments", "command", "cmd", "shell"}
)

func loadToolCatalog(t *testing.T) toolCatalog {
	t.Helper()
	raw, err := os.ReadFile(toolCatalogFile)
	if err != nil {
		t.Fatalf("read %s: %v", toolCatalogFile, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c toolCatalog
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("decode %s: %v", toolCatalogFile, err)
	}
	return c
}

func TestToolCatalogV1(t *testing.T) {
	c := loadToolCatalog(t)
	if c.CatalogVersion != 1 {
		t.Fatalf("catalog_version = %d, want 1", c.CatalogVersion)
	}
	declared := slices.Sorted(maps.Keys(c.Types))
	if want := slices.Sorted(slices.Values(slices.Concat(actionArgTypes, uiArgTypes))); !slices.Equal(declared, want) {
		t.Fatalf("declared argument types are %v, want %v", declared, want)
	}
	if len(c.Tools) == 0 {
		t.Fatal("the catalog has no tools")
	}

	names := map[string]bool{}
	var gotUI []string
	for _, tool := range c.Tools {
		if !toolNamePattern.MatchString(tool.Name) {
			t.Errorf("tool name %q is not snake_case of at most 64 characters", tool.Name)
		}
		if names[tool.Name] {
			t.Errorf("tool %q is declared twice", tool.Name)
		}
		names[tool.Name] = true
		if !slices.Contains(toolClasses, tool.Class) {
			t.Errorf("tool %q has class %q, want one of %v", tool.Name, tool.Class, toolClasses)
		}
		// Class C exists only as a confirm card: the student's click executes it, the agent never does.
		if tool.AgentCanExecute == (tool.Class == "C") {
			t.Errorf("tool %q of class %s: agent_can_execute = %v", tool.Name, tool.Class, tool.AgentCanExecute)
		}
		if tool.Paid && (tool.Class == "C" || tool.Class == "UI") {
			t.Errorf("tool %q of class %s cannot be paid", tool.Name, tool.Class)
		}
		if tool.Class == "UI" {
			gotUI = append(gotUI, tool.Name)
		}

		allowed := actionArgTypes
		switch tool.Class {
		case "UI":
			allowed = uiArgTypes
		case "C":
			allowed = confirmArgTypes
		}
		argNames := map[string]bool{}
		for _, a := range tool.Arguments {
			where := tool.Name + "." + a.Name
			if !toolNamePattern.MatchString(a.Name) {
				t.Errorf("argument %s is not snake_case", where)
			}
			if argNames[a.Name] {
				t.Errorf("argument %s is declared twice", where)
			}
			argNames[a.Name] = true
			words := strings.Split(a.Name, "_")
			if badName(a.Name) || badAgentWords(words) || slices.ContainsFunc(words, func(w string) bool {
				return slices.Contains(forbiddenArgWords, w)
			}) || slices.Contains(forbiddenArgWords, a.Name) {
				t.Errorf("argument %s names a destination, a recipient, a path or a file (ADR-0022)", where)
			}
			if !slices.Contains(allowed, a.Type) {
				t.Errorf("argument %s has type %q, a %s tool allows only %v", where, a.Type, tool.Class, allowed)
			}
			if (a.Type == "enum") != (len(a.Values) > 0) {
				t.Errorf("argument %s: an enum needs values and only an enum has them", where)
			}
			for i, v := range a.Values {
				if !toolNamePattern.MatchString(v) || slices.Contains(a.Values[:i], v) {
					t.Errorf("argument %s: enum value %q is not snake_case or repeats", where, v)
				}
			}
		}
	}
	slices.Sort(gotUI)
	if !slices.Equal(gotUI, uiTools) {
		t.Errorf("UI tools are %v, want %v", gotUI, uiTools)
	}
}

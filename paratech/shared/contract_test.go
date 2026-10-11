package shared_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"

	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

const hubPackage = "paratech.hub.v1"

// Words that must not appear as a part of any field name of the Hub contract. A client must never choose where the Hub
// connects (PRD section 13.2, hard rules), and credentials travel in a header, not in message bodies (PRD section 12).
// Names are matched per snake_case word, so "duration" or "security" are fine.
var (
	forbiddenWords = []string{
		"url", "urls", "uri", "uris", "endpoint", "endpoints", "host", "hosts", "hostname", "baseurl",
		"secret", "password", "bearer", "authorization", "credential", "credentials", "apikey",
	}
	// Forbidden as a consecutive pair of words.
	forbiddenPairs = [][2]string{{"api", "key"}, {"base", "url"}, {"access", "token"}}
)

// forbiddenFieldNames returns "Message.field" for every field (nested messages included) whose name contains a forbidden
// word, and for every float or double field (quota amounts and token counts are exact integers).
func forbiddenFieldNames(files ...protoreflect.FileDescriptor) []string {
	var found []string
	var walk func(protoreflect.MessageDescriptors)
	walk = func(msgs protoreflect.MessageDescriptors) {
		for i := range msgs.Len() {
			m := msgs.Get(i)
			fields := m.Fields()
			for j := range fields.Len() {
				f := fields.Get(j)
				name := string(f.Name())
				if badName(name) || f.Kind() == protoreflect.FloatKind || f.Kind() == protoreflect.DoubleKind {
					found = append(found, string(m.FullName())+"."+name)
				}
			}
			walk(m.Messages())
		}
	}
	for _, f := range files {
		walk(f.Messages())
	}
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
		t.Fatalf("fields that let a client choose a destination, carry a secret, or use floating point: %v", bad)
	}
}

// The checker itself must be able to fail, otherwise the test above proves nothing.
func TestForbiddenFieldCheckerCatchesBadFields(t *testing.T) {
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	dbl := descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum()
	field := func(name string, n int32, typ *descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name: proto.String(name), Number: proto.Int32(n), Type: typ,
			Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		}
	}
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("synthetic.proto"),
		Package: proto.String("synthetic.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Bad"),
			Field: []*descriptorpb.FieldDescriptorProto{
				field("base_url", 1, str),
				field("callback_uri", 2, str),
				field("provider_host", 3, str),
				field("Endpoint", 4, str),
				field("api_key", 5, str),
				field("price", 6, dbl),
				// Look-alikes that are fine: the word is only a substring.
				field("duration", 7, str),
				field("security_level", 8, str),
				field("ghost", 9, str),
			},
			NestedType: []*descriptorpb.DescriptorProto{{
				Name:  proto.String("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{field("upstream_url", 1, str)},
			}},
		}},
	}
	fd, err := protodesc.NewFile(fdp, nil)
	if err != nil {
		t.Fatalf("build synthetic descriptor: %v", err)
	}
	got := forbiddenFieldNames(fd)
	want := []string{
		"synthetic.v1.Bad.base_url", "synthetic.v1.Bad.callback_uri", "synthetic.v1.Bad.provider_host",
		"synthetic.v1.Bad.Endpoint", "synthetic.v1.Bad.api_key", "synthetic.v1.Bad.price", "synthetic.v1.Bad.Inner.upstream_url",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("checker found %v, want %v", got, want)
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

func TestGatewayAndQuotaServicesShape(t *testing.T) {
	want := map[string]map[string]bool{ // service -> method -> server streaming
		"GatewayService": {"Complete": false, "CompleteStream": true},
		"QuotaService":   {"Reserve": false, "Settle": false, "Release": false, "GetRemaining": false},
	}
	for svcName, methods := range want {
		d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(hubPackage + "." + svcName))
		if err != nil {
			t.Fatalf("service %s: %v", svcName, err)
		}
		svc := d.(protoreflect.ServiceDescriptor)
		if svc.Methods().Len() != len(methods) {
			t.Errorf("%s has %d methods, want %d", svcName, svc.Methods().Len(), len(methods))
		}
		for name, streaming := range methods {
			m := svc.Methods().ByName(protoreflect.Name(name))
			if m == nil {
				t.Errorf("%s.%s is missing", svcName, name)
				continue
			}
			if m.IsStreamingServer() != streaming || m.IsStreamingClient() {
				t.Errorf("%s.%s: server streaming = %v, client streaming = %v, want server streaming %v and no client streaming",
					svcName, name, m.IsStreamingServer(), m.IsStreamingClient(), streaming)
			}
		}
	}
}

func TestIdempotencyAndModelAliasFields(t *testing.T) {
	mustHave := func(msg, field string) {
		t.Helper()
		d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(hubPackage + "." + msg))
		if err != nil {
			t.Fatalf("message %s: %v", msg, err)
		}
		if d.(protoreflect.MessageDescriptor).Fields().ByName(protoreflect.Name(field)) == nil {
			t.Errorf("%s.%s is missing", msg, field)
		}
	}
	for _, f := range []string{"idempotency_key", "operation_type", "model_alias", "instructions", "untrusted_data"} {
		mustHave("CompletionSpec", f)
	}
	for _, f := range []string{"idempotency_key", "operation_type", "model_alias", "amount"} {
		mustHave("ReserveRequest", f)
	}
	mustHave("SettleRequest", "reservation_id")
	mustHave("ReleaseRequest", "reservation_id")
}

// The doc comments promise that the server attaches an ErrorDetail to the Connect error and that the client reads it
// back; make sure that works with the generated types.
func TestErrorDetailTransportsOverConnectError(t *testing.T) {
	detail := &hubv1.ErrorDetail{
		Code:       hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED,
		RetryAfter: durationpb.New(7 * time.Second),
		Message:    "slow down",
	}
	d, err := connect.NewErrorDetail(detail)
	if err != nil {
		t.Fatalf("NewErrorDetail: %v", err)
	}
	sent := connect.NewError(connect.CodeResourceExhausted, errors.New("rate limited"))
	sent.AddDetail(d)

	var got *connect.Error
	if !errors.As(error(sent), &got) || len(got.Details()) != 1 {
		t.Fatalf("expected one detail on the connect error, got %+v", got)
	}
	value, err := got.Details()[0].Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	read, ok := value.(*hubv1.ErrorDetail)
	if !ok {
		t.Fatalf("detail is %T, want *ErrorDetail", value)
	}
	if read.GetCode() != hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED || read.GetRetryAfter().AsDuration() != 7*time.Second {
		t.Fatalf("detail changed in transit: %v", read)
	}
}

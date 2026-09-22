package option

import (
	"context"
	"reflect"
	"testing"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/schema"
	"github.com/sagernet/sing/common/json"

	"github.com/stretchr/testify/require"
)

func TestXHTTPTransportOptions(t *testing.T) {
	input := `{"type":"xhttp","host":"example.org","path":"/test","mode":"stream-up","http_version":"2","max_post_size":65536,"headers":{"X-Test":"value"}}`
	var options V2RayTransportOptions
	require.NoError(t, json.Unmarshal([]byte(input), &options))
	require.Equal(t, C.V2RayTransportTypeXHTTP, options.Type)
	require.Equal(t, "stream-up", options.XHTTPOptions.Mode)
	require.Equal(t, 65536, options.XHTTPOptions.MaxPostSize)
	encoded, err := json.Marshal(options)
	require.NoError(t, err)
	var decoded V2RayTransportOptions
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, options, decoded)
	require.Error(t, json.Unmarshal([]byte(`{"type":"xhttp","extra":{"noGRPCHeader":true}}`), &decoded))
	generated, err := schema.Generate(context.Background(), reflect.TypeFor[V2RayTransportOptions]())
	require.NoError(t, err)
	require.Contains(t, string(generated), `"xhttp"`)
	require.Contains(t, string(generated), `"max_post_size"`)
}

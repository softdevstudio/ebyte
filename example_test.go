package ebyte_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/softdevstudio/ebyte"
)

func ExamplePlan_Encode() {
	schema := []byte(`{"schema_version":1,"name":"example","endian":"little","fields":[{"name":"n","type":"u8","derive":{"count_of":"items"}},{"name":"items","type":"array","count":{"field":"n"},"fields":[{"name":"id","type":"u16"}]}]}`)
	plan, err := ebyte.Compile(schema, ebyte.DefaultLimits())
	if err != nil {
		panic(err)
	}
	document, _, err := plan.Decode(context.Background(), bytes.NewReader([]byte{2, 1, 0, 2, 0}))
	if err != nil {
		panic(err)
	}
	document.Values["items"] = append(document.Values["items"].([]any), map[string]any{"id": "3"})
	var out bytes.Buffer
	if err = plan.Encode(context.Background(), document, &out); err != nil {
		panic(err)
	}
	fmt.Printf("%x\n", out.Bytes())
	// Output: 03010002000300
}

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestProviderMetadata(t *testing.T) {
	var resp provider.MetadataResponse
	New("test")().Metadata(context.Background(), provider.MetadataRequest{}, &resp)
	if resp.TypeName != "optimizelycmp" {
		t.Fatalf("unexpected type name %q", resp.TypeName)
	}
}

func TestProviderRegistersResourcesAndLookups(t *testing.T) {
	p := New("test")().(*cmpProvider)
	if n := len(p.Resources(context.Background())); n != 8 {
		t.Errorf("resources = %d, want 8", n)
	}
	if n := len(p.DataSources(context.Background())); n != 11 {
		t.Errorf("data sources = %d, want 11", n)
	}
}

func TestSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := New("test")().(*cmpProvider)
	for _, f := range p.Resources(ctx) {
		r := f()
		var m resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "optimizelycmp"}, &m)
		var s resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &s)
		if d := s.Schema.ValidateImplementation(ctx); d.HasError() {
			t.Errorf("%s: %v", m.TypeName, d)
		}
	}
	for _, f := range p.DataSources(ctx) {
		d := f()
		var m datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "optimizelycmp"}, &m)
		var s datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &s)
		if diags := s.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: %v", m.TypeName, diags)
		}
	}
}

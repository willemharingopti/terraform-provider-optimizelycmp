package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/provider"
)

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name optimizelycmp

var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run provider with debugger support")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/willemharingopti/optimizelycmp",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}

terraform {
  required_providers {
    optimizelycmp = {
      source = "willemharingopti/optimizelycmp" # replace with your registry address
    }
  }
}

# Credentials are read from the environment (recommended; keeps the secret out of config and state):
#   export OPTIMIZELY_CMP_CLIENT_ID=...
#   export OPTIMIZELY_CMP_CLIENT_SECRET=...
#   export OPTIMIZELY_CMP_BASE_URL=https://api.cmp.optimizely.com   # optional
provider "optimizelycmp" {}

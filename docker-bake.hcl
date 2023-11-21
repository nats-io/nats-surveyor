###################
### Variables
###################

variable REGISTRY {
  default = ""
}

# Comma delimited list of tags
variable TAGS {
  default = "latest"
}

variable CI {
  default = false
}

###################
### Functions
###################

function "get_tags" {
  params = [image]
  result = [for tag in split(",", TAGS) : join("/", compact([REGISTRY, "${image}:${tag}"]))]
}

function "get_platforms_multiarch" {
  params = []
  result = CI ? ["linux/amd64", "linux/arm/v6", "linux/arm/v7", "linux/arm64"] : []
}

function "get_output" {
  params = []
  result = CI ? ["type=registry"] : ["type=docker"]
}

###################
### Groups
###################

group "default" {
  targets = [
    "nats-surveyor"
  ]
}

###################
### Targets
###################

target "goreleaser" {
  contexts = {
    src = "."
  }
  args = {
    CI = CI
    GITHUB_TOKEN = ""
  }
  dockerfile = "cicd/Dockerfile_goreleaser"
}

target "nats-surveyor" {
  contexts = {
    assets     = "cicd/assets"
    goreleaser = "target:goreleaser"
  }
  args = {
    GO_APP = "nats-surveyor"
  }
  dockerfile  = "cicd/Dockerfile"
  platforms   = get_platforms_multiarch()
  tags        = get_tags("nats-surveyor")
  output      = get_output()
}

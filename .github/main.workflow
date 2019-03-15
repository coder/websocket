workflow "main" {
  on = "push"
  resolves = ["deps", "fmt", "vet", "test"]
}

action "deps" {
  uses = "./.github/deps"
}

action "fmt" {
  uses = "./.github/fmt"
}

action "vet" {
  uses = "./.github/vet"
}

action "test" {
  uses = "./.github/test"
  secrets = ["CODECOV_TOKEN"]
}

workflow "main" {
  on = "push"
  resolves = ["fmt", "test"]
}

action "fmt" {
  uses = "./.github/fmt"
}

action "test" {
  uses = "./.github/test"
  secrets = ["CODECOV_TOKEN"]
}

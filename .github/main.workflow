workflow "main" {
  on = "push"
  resolves = ["fmt", "lint", "test"]
}

action "lint" {
  uses = "./.github/lint"
}

action "fmt" {
  uses = "./.github/fmt"
}

action "test" {
  uses = "./.github/test"
  secrets = ["CODECOV_TOKEN"]
}

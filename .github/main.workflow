workflow "main" {
  on = "push"
  resolves = ["fmt", "lint", "test", "bench"]
}

action "lint" {
  uses = "./ci/lint"
}

action "fmt" {
  uses = "./ci/fmt"
}

action "test" {
  uses = "./ci/test"
  secrets = ["CODECOV_TOKEN"]
}

action "bench" {
  uses = "./ci/bench"
}

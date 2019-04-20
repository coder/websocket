workflow "main" {
  on = "push"
  resolves = ["fmt", "lint", "test", "bench"]
}

action "lint" {
  uses = "../test/lint"
}

action "fmt" {
  uses = "../test/fmt"
}

action "test" {
  uses = "../test/test"
  secrets = ["CODECOV_TOKEN"]
}

action "bench" {
  uses = "../test/bench"
}

workflow "main" {
  on = "push"
  resolves = ["test"]
}

action "test" {
  uses = ".github/test"
}

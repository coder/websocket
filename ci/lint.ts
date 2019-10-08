#!/usr/bin/env -S npx ts-node -P ci/tsconfig.json

import { exec, main, wasmEnv } from "./lib"

if (require.main === module) {
  main(lint)
}

export async function lint(ctx: Promise<unknown>) {
  await Promise.all([
    exec(ctx, "go vet ./..."),
    exec(ctx, "go run golang.org/x/lint/golint -set_exit_status ./..."),
    exec(ctx, "git ls-files '*.ts' | xargs npx eslint --max-warnings 0 --fix", {
      cwd: "ci",
    }),
    wasmLint(ctx),
  ])
}

async function wasmLint(ctx: Promise<unknown>) {
  await exec(ctx, "go install golang.org/x/lint/golint")
  await exec(ctx, "golint -set_exit_status ./...", {
    env: wasmEnv,
  })
}

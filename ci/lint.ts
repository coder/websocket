#!/usr/bin/env -S npx ts-node -P ci/tsconfig.json

import { exec, main } from "./lib"

if (process.argv[1] === __filename) {
  main(lint)
}

export async function lint(ctx: Promise<unknown>) {
  await Promise.all([
      exec(ctx, "go vet ./..."),
      exec(ctx, "go run golang.org/x/lint/golint -set_exit_status ./..."),
      exec(ctx, "git ls-files '*.ts' | xargs npx eslint --max-warnings 0 --fix", {
        cwd: "ci",
      }),
    ],
  )
}

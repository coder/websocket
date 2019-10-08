#!/usr/bin/env -S npx ts-node -P ci/tsconfig.json

import { exec, main } from "./lib"

if (require.main === module) {
  main(async (ctx: Promise<unknown>) => {
    await gen(ctx)
    await fmt(ctx)
  })
}

export async function fmt(ctx: Promise<unknown>) {
  await Promise.all([
    exec(ctx, "go mod tidy"),
    exec(ctx, "gofmt -w -s ."),
    exec(ctx, `go run go.coder.com/go-tools/cmd/goimports -w "-local=$(go list -m)" .`),
    exec(
      ctx,
      `npx prettier --write --print-width=120 --no-semi --trailing-comma=all --loglevel=silent $(git ls-files "*.yaml" "*.yml" "*.md" "*.ts")`,
    ),
  ])

  if (process.env.CI) {
    const r = await exec(ctx, "git ls-files --other --modified --exclude-standard")
    const files = r.stdout.toString().trim()
    if (files.length) {
      console.log(`files need generation or are formatted incorrectly:
${files}

please run:
  ./ci/fmt.js`)
      process.exit(1)
    }
  }
}

export async function gen(ctx: Promise<unknown>) {
  await exec(ctx, "go generate ./...")
}

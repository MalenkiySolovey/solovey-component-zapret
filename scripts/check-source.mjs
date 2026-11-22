import assert from 'node:assert/strict'
import fs from 'node:fs'
import crypto from 'node:crypto'
import path from 'node:path'
import {fileURLToPath} from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'component.json'), 'utf8'))
assert.equal(manifest.id, 'zapret')
assert.equal(manifest.delivery, 'in-process')
assert.equal(manifest.defaultEnabled, false)
assert.deepEqual(manifest.frontend.entries, ['frontend/index.ts'])
assert.deepEqual(manifest.hostAPI, {version: 'solovey.component-host/v1', api: ['http.request']})
const upstream = JSON.parse(fs.readFileSync(path.join(root, 'upstream/zapret2.json'), 'utf8'))
assert.equal(upstream.repository, 'https://github.com/bol-van/zapret2.git')
assert.match(upstream.commit, /^[a-f0-9]{40}$/)
assert.match(upstream.tree, /^[a-f0-9]{40}$/)
assert.equal(upstream.tag, 'v1.0.7')
assert.match(upstream.archiveSHA256, /^[a-f0-9]{64}$/)
assert.equal(upstream.license, 'MIT')
for (const filename of ['component.json', 'manifest.go', 'lifecycle.go', 'strategy/static.go', 'binding/resolve.go', 'LICENSE', 'NOTICE', 'SOURCE_PROVENANCE.json']) {
  assert.ok(fs.statSync(path.join(root, filename)).isFile(), `missing source file: ${filename}`)
}
const source = fs.readFileSync(path.join(root, 'strategy/static.go'), 'utf8') + fs.readFileSync(path.join(root, 'binding/resolve.go'), 'utf8')
assert.ok(!/os\/exec|syscall|net\.Listen|iptables|nft\s/.test(source), 'semantic component owns a privileged executor')
assert.ok(!/go\s+func|time\.NewTicker|net\.Dial/.test(source), 'semantic component owns a background worker or network client')
assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(root, 'LICENSE'))).digest('hex'), '3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986')
console.log(JSON.stringify({status: 'PASS', component: manifest.id, upstreamCommit: upstream.commit, upstreamTag: upstream.tag}))

-- A separate native Lua environment for each selected strategy. Do not add
-- functions that can reach the trusted global environment, filesystem or
-- arbitrary network destinations, including indirectly through packet helpers.
local engine = _G
local base = {}
for _, name in ipairs({
    "assert", "error", "ipairs", "next", "pairs", "pcall", "rawequal", "rawget",
    "rawlen", "rawset", "select", "setmetatable", "tonumber", "tostring", "type", "xpcall",
    "VERDICT_PASS", "VERDICT_DROP",
}) do
    base[name] = engine[name]
end

-- These are the functions registered by the pinned nfqws2 Lua API. Keep the
-- original engine functions and argument semantics; the administrator's
-- explicit Host delegation is the authority boundary. Host file loaders,
-- package search and shell-capable standard-library entry points remain out of
-- the managed environment because the bundle is intentionally sealed and
-- source-selected by the broker.
for _, name in ipairs({
    "DLOG_ERR", "DLOG_CONDUP", "ntop", "pton",
    "bitlshift", "bitrshift", "bitand", "bitor", "bitxor", "bitget", "bitset",
    "bitnot", "bitnot8", "bitnot16", "bitnot24", "bitnot32", "bitnot48",
    "u8", "u16", "u24", "u32", "u48", "u8add", "u16add", "u24add", "u32add", "u48add",
    "bu8", "bu16", "bu24", "bu32", "bu48", "swap16", "swap24", "swap32", "swap48",
    "divint", "brandom", "brandom_az", "brandom_az09", "bcryptorandom", "bxor", "bor", "band",
    "hash", "aes", "aes_gcm", "aes_ctr", "hkdf", "parse_hex",
    "instance_cutoff", "lua_cutoff", "execution_plan", "execution_plan_cancel", "raw_packet",
    "uname", "clock_gettime", "clock_getfloattime", "getpid", "gettid",
    "reconstruct_tcphdr", "reconstruct_udphdr", "reconstruct_icmphdr", "reconstruct_ip6hdr",
    "reconstruct_iphdr", "reconstruct_dissect", "dissect_tcphdr", "dissect_udphdr",
    "dissect_icmphdr", "dissect_ip6hdr", "dissect_iphdr", "dissect",
    "csum_ip4_fix", "csum_tcp_fix", "csum_udp_fix", "csum_icmp_fix",
    "rawsend", "rawsend_dissect", "conntrack_feed", "get_source_ip", "get_ifaddrs",
    "resolve_pos", "resolve_multi_pos", "resolve_range", "tls_mod",
    "gunzip_init", "gunzip_end", "gunzip_inflate", "gzip_init", "gzip_end", "gzip_deflate",
    "stat", "localtime", "gmtime", "timelocal", "timegm",
    "timer_set", "timer_del", "timer_info", "timer_enum",
}) do
    base[name] = engine[name]
end
for _, name in ipairs({"math", "string", "table", "utf8"}) do
    local copy = {}
    for k, v in pairs(engine[name]) do copy[k] = v end
    base[name] = copy
end
base.os = {clock = os.clock, date = os.date, difftime = os.difftime, time = os.time}
base.getmetatable = function(value)
    if type(value) == "table" then return getmetatable(value) end
end
base.DLOG = function(message)
    if type(message) == "string" then
        DLOG("managed Lua: " .. message:sub(1, 240):gsub("%c", " "))
    end
end

local env = setmetatable({}, {__index = base, __metatable = false})
env._G = env
local loaded = {}
base.require = function(name)
    local value = loaded[name]
    if value == nil then error("unselected or not yet loaded managed Lua dependency", 2) end
    return value
end

local function solovey_load(id, source)
    -- Match Lua's file loader while retaining the original bytes in storage.
    if source:sub(1, 3) == "\239\187\191" then source = source:sub(4) end
    if source:sub(1, 1) == "#" then
        local line_end = source:find("\n", 1, true)
        source = line_end and source:sub(line_end) or "\n"
    end
    local chunk = assert(load(source, "@managed/" .. id .. ".lua", "t", env))
    local result = chunk()
    if result == nil then result = true end
    loaded[id] = result
end

-- The callback can inspect the selected packet and choose its own verdict.
-- Never hand it a reference to shared conntrack state from built-in actions.
local function packet_view(value, seen)
    local kind = type(value)
    if kind == "string" or kind == "number" or kind == "boolean" then return value end
    if kind ~= "table" then return nil end
    if seen[value] then return seen[value] end
    local result = {}
    seen[value] = result
    for key, field in next, value do
        local keytype = type(key)
        if key ~= "lua_state" and (keytype == "string" or keytype == "number") then
            result[key] = packet_view(field, seen)
        end
    end
    return result
end

local flow_states = setmetatable({}, {__mode = "k"})

local function action_view(desync)
    local view = packet_view(desync, {})
    if desync and desync.track and type(desync.track.lua_state) == "table" then
        local flow = desync.track.lua_state
        if not flow_states[flow] then flow_states[flow] = {} end
        view.track.lua_state = flow_states[flow]
    end
    return view
end

local function solovey_bind(entry, name)
    local callback = rawget(env, name)
    if type(callback) ~= "function" then error("managed Lua function not found: " .. name) end
    if engine[entry] ~= nil then error("duplicate managed Lua entry") end
    local reported = false
    engine[entry] = function(ctx, desync)
        local ok, verdict = pcall(callback, ctx, action_view(desync))
        if ok and (verdict == nil or verdict == VERDICT_PASS or verdict == VERDICT_DROP) then
            return verdict
        end
        if not reported then
            reported = true
            DLOG_ERR("managed Lua action failed or returned an unsupported verdict: " .. name)
        end
        return VERDICT_DROP
    end
end

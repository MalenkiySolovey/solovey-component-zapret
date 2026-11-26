-- A separate native Lua environment for each selected strategy. Do not add
-- functions that can reach the trusted global environment, filesystem or
-- arbitrary network destinations, including indirectly through packet helpers.
local engine = _G
local base = {}
for _, name in ipairs({
    "assert", "error", "ipairs", "next", "pairs", "pcall", "rawequal", "rawget",
    "rawlen", "rawset", "select", "setmetatable", "tonumber", "tostring", "type", "xpcall",
    "VERDICT_PASS", "VERDICT_DROP", "raw_packet", "resolve_pos", "resolve_multi_pos", "resolve_range",
    "dissect", "dissect_tcphdr", "dissect_udphdr", "dissect_icmphdr", "dissect_iphdr", "dissect_ip6hdr",
    "reconstruct_dissect", "reconstruct_tcphdr", "reconstruct_udphdr", "reconstruct_icmphdr",
    "reconstruct_iphdr", "reconstruct_ip6hdr", "parse_hex", "tls_mod", "pton", "ntop",
    "u8", "u16", "u24", "u32", "u48", "bu8", "bu16", "bu24", "bu32", "bu48",
    "u8add", "u16add", "u24add", "u32add", "u48add", "swap16", "swap24", "swap32", "swap48",
    "brandom", "brandom_az", "brandom_az09", "bxor", "bor", "band", "hash",
    "clock_gettime", "clock_getfloattime", "localtime", "gmtime", "timelocal", "timegm",
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

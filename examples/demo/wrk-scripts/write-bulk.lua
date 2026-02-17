-- wrk script for bulk document writes
-- Usage: wrk -t4 -c20 -d30s --latency -s write-bulk.lua http://localhost:8091/indices/stress-test/_bulk

local counter = 0
local thread_id = 0
local bulk_size = 100  -- documents per bulk request

function setup(thread)
    thread:set("id", thread_id)
    thread_id = thread_id + 1
end

function init(args)
    counter = id * 10000000
end

request = function()
    local lines = {}
    
    for i = 1, bulk_size do
        counter = counter + 1
        local doc = string.format('{"id":%d,"value":%d,"category":"cat_%d","timestamp":"%s","message":"Bulk doc %d"}',
            counter, math.random(1, 1000), math.random(1, 10), 
            os.date("!%Y-%m-%dT%H:%M:%SZ"), counter)
        table.insert(lines, doc)
    end
    
    local body = table.concat(lines, "\n") .. "\n"
    
    return wrk.format("POST", nil, {
        ["Content-Type"] = "application/x-ndjson",
        ["Authorization"] = "Bearer " .. os.getenv("AUTH_TOKEN")
    }, body)
end

response = function(status, headers, body)
    if status ~= 200 and status ~= 201 then
        print("Error status: " .. status .. " body: " .. body)
    end
end

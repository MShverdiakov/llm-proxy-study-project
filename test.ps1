
# LLM Proxy System - Smoke Test
# Run: .\test.ps1

$ErrorActionPreference = "Stop"

$AUTH_URL    = "http://localhost:8001"
$BILLING_URL = "http://localhost:8002"
$PROXY_URL   = "http://localhost:8080"

function Ok   { param($msg) Write-Host "[OK]   $msg" -ForegroundColor Green }
function Fail { param($msg) Write-Host "[FAIL] $msg" -ForegroundColor Red; exit 1 }
function Info { param($msg) Write-Host "" ; Write-Host ">>>> $msg" -ForegroundColor Yellow }

function Invoke-Api {
    param($Method, $Url, $Body, [hashtable]$Headers = @{})
    $params = @{
        Method      = $Method
        Uri         = $Url
        Headers     = $Headers
        ContentType = "application/json"
        ErrorAction = "Stop"
    }
    if ($Body) { $params.Body = ($Body | ConvertTo-Json -Compress) }
    try {
        return Invoke-RestMethod @params
    } catch {
        $status = $_.Exception.Response.StatusCode.value__
        return [PSCustomObject]@{ error = $_.ToString(); status = $status }
    }
}

function Get-StatusCode {
    param($Method, $Url, $Body, [hashtable]$Headers = @{})
    $params = @{
        Method      = $Method
        Uri         = $Url
        Headers     = $Headers
        ContentType = "application/json"
        ErrorAction = "SilentlyContinue"
    }
    if ($Body) { $params.Body = ($Body | ConvertTo-Json -Compress) }
    try {
        Invoke-RestMethod @params | Out-Null
        return 200
    } catch {
        return $_.Exception.Response.StatusCode.value__
    }
}

# --- 1. Health checks ---
Info "1. Health checks"

try { Invoke-RestMethod "$AUTH_URL/health" | Out-Null;    Ok "auth-service /health" }    catch { Fail "auth-service not available" }
try { Invoke-RestMethod "$BILLING_URL/health" | Out-Null; Ok "billing-service /health" } catch { Fail "billing-service not available" }
try { Invoke-RestMethod "$PROXY_URL/health" | Out-Null;   Ok "llm-proxy /health (via HAProxy)" } catch { Fail "llm-proxy not available" }

# --- 2. Register ---
Info "2. Register user"

$registerBody = @{ email = "smoketest@example.com"; password = "secret123" }
$register = Invoke-Api -Method POST -Url "$AUTH_URL/auth/register" -Body $registerBody

$API_KEY = $register.api_key
$USER_ID = $register.user.ID

Write-Host "  api_key : $API_KEY"
Write-Host "  user_id : $USER_ID"

if (-not $API_KEY) { Fail "api_key not received" }
if (-not $USER_ID) { Fail "user_id not received" }
Ok "User registered: $($register.user.Email)"

# --- 3. Login ---
Info "3. Login"

$loginBody = @{ email = "smoketest@example.com"; password = "secret123" }
$login = Invoke-Api -Method POST -Url "$AUTH_URL/auth/login" -Body $loginBody

if (-not $login.jwt_token) { Fail "JWT not received" }
Ok "JWT received: $($login.jwt_token.Substring(0, 20))..."

# --- 4. Validate API key ---
Info "4. Validate API key"

$validate = Invoke-RestMethod "$AUTH_URL/auth/validate?api_key=$API_KEY"
Write-Host "  $($validate | ConvertTo-Json -Compress)"
if (-not $validate.user_id) { Fail "API key invalid" }
Ok "API key valid, user_id: $($validate.user_id)"

# --- 5. Deposit ---
Info "5. Deposit balance"

$depositBody = @{ user_id = $USER_ID; amount = 1000 }
$deposit = Invoke-Api -Method POST -Url "$BILLING_URL/billing/deposit" -Body $depositBody

Write-Host "  $($deposit | ConvertTo-Json -Compress)"
if ($deposit.balance -ne 1000) { Fail "Deposit failed" }
Ok "Balance deposited: $($deposit.balance)"

# --- 6. Check balance ---
Info "6. Check balance"

$balance = Invoke-RestMethod "$BILLING_URL/billing/balance/$USER_ID"
Write-Host "  $($balance | ConvertTo-Json -Compress)"
if ($balance.balance -ne 1000) { Fail "Wrong balance" }
Ok "Balance: $($balance.balance)"

# --- 7. LLM completion #1 (cache miss) ---
Info "7. LLM completion #1 (cache miss)"

$completionBody = @{
    model    = "mock-gpt-4"
    messages = @(@{ role = "user"; content = "What is 2+2?" })
}
$headers = @{ Authorization = "Bearer $API_KEY" }

$resp1 = Invoke-Api -Method POST -Url "$PROXY_URL/completions" -Body $completionBody -Headers $headers

Write-Host "  content  : $($resp1.content)"
Write-Host "  model    : $($resp1.model)"
Write-Host "  tokens   : $($resp1.usage.total_tokens)"
Write-Host "  latency  : $($resp1.latency_ms)ms"

if (-not $resp1.content) { Fail "No response received" }
Ok "Completion OK, latency: $($resp1.latency_ms)ms"

# --- 8. LLM completion #2 (cache hit) ---
Info "8. LLM completion #2 (cache hit - same request)"

$resp2 = Invoke-Api -Method POST -Url "$PROXY_URL/completions" -Body $completionBody -Headers $headers

Write-Host "  content  : $($resp2.content)"
Write-Host "  latency  : $($resp2.latency_ms)ms"

if ($resp2.content -ne $resp1.content) { Fail "Cache response differs from original" }
Ok "Cache hit confirmed (latency: $($resp2.latency_ms)ms vs $($resp1.latency_ms)ms)"

# --- 9. Balance after usage ---
Info "9. Balance after usage (waiting for RabbitMQ consumer)"

Start-Sleep -Seconds 2

$balance2 = Invoke-RestMethod "$BILLING_URL/billing/balance/$USER_ID"
Write-Host "  Balance was: 1000, now: $($balance2.balance)"
Ok "Balance after request: $($balance2.balance)"

# --- 10. Transaction history ---
Info "10. Transaction history"

$txs = Invoke-RestMethod "$BILLING_URL/billing/transactions/$USER_ID"
Write-Host "  Count: $($txs.Count)"
foreach ($tx in $txs) {
    Write-Host "    type=$($tx.type)  amount=$($tx.amount)  model=$($tx.model)"
}
if ($txs.Count -eq 0) { Fail "No transactions" }
Ok "Transactions: $($txs.Count)"

# --- 11. List models ---
Info "11. GET /models"

$models = Invoke-RestMethod "$PROXY_URL/models"
Write-Host "  $($models | ConvertTo-Json -Compress)"
if ($models.Count -eq 0) { Fail "No models returned" }
Ok "Models: $($models.Count)"

# --- 12. Usage stats ---
Info "12. GET /billing/usage"

$usage = Invoke-RestMethod "$BILLING_URL/billing/usage?user_id=$USER_ID&period=day"
Write-Host "  $($usage | ConvertTo-Json -Compress)"
Ok "Usage today: requests=$($usage.Requests) tokens=$($usage.TotalTokens)"

# --- 13. Stats endpoints ---
Info "13. Stats endpoints"

$authStats    = Invoke-RestMethod "$AUTH_URL/stats"
$billingStats = Invoke-RestMethod "$BILLING_URL/stats"

Ok "auth    /stats : uptime=$($authStats.uptime_seconds)s  requests=$($authStats.requests.total)"
Ok "billing /stats : uptime=$($billingStats.uptime_seconds)s  requests=$($billingStats.requests.total)"

# --- 14. 401 on bad key ---
Info "14. 401 on invalid API key"

$badHeaders = @{ Authorization = "Bearer invalidkeyinvalidkey" }
$status401 = Get-StatusCode -Method POST -Url "$PROXY_URL/completions" -Body $completionBody -Headers $badHeaders
if ($status401 -eq 401) { Ok "401 received correctly (status: $status401)" } else { Fail "Expected 401, got $status401" }

# --- 15. 402 on zero balance ---
Info "15. 402 on zero balance"

$brokeBody = @{ email = "broke@example.com"; password = "broke" }
$brokeUser = Invoke-Api -Method POST -Url "$AUTH_URL/auth/register" -Body $brokeBody
$brokeKey  = $brokeUser.api_key
$brokeHeaders = @{ Authorization = "Bearer $brokeKey" }

$status402 = Get-StatusCode -Method POST -Url "$PROXY_URL/completions" -Body $completionBody -Headers $brokeHeaders
if ($status402 -eq 402) { Ok "402 received correctly (no balance)" } else { Fail "Expected 402, got $status402" }

# --- 16. Clear cache ---
Info "16. DELETE /cache"

$status204 = Get-StatusCode -Method DELETE -Url "$PROXY_URL/cache"
if ($status204 -eq 204) { Ok "Cache cleared (204)" } else { Fail "Expected 204, got $status204" }

# --- HAProxy round-robin ---
Info "17. HAProxy round-robin (6 requests)"

for ($i = 1; $i -le 6; $i++) {
    $r = Invoke-Api -Method POST -Url "$PROXY_URL/completions" -Body $completionBody -Headers $headers
    $preview = $r.content.Substring(0, [Math]::Min(40, $r.content.Length))
    Write-Host "  Request $i`: $preview"
}
Ok "6 requests sent via HAProxy"

# --- Done ---
Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "  All tests passed!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green

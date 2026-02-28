#!/usr/bin/env bash
# Reproduces the predictor's PuckPedia request so you can verify if the page
# returns HTML that contains the Caps game and goalie names (e.g. Washington + Montreal, Lindgren, Dobes).
# Usage: ./scripts/check-puckpedia-goalie.sh [output_file]
# If output_file is given, full response is saved there for inspection.

URL="https://depth-charts.puckpedia.com/starting-goalies?dayCount=2&timezone=America/New_York"
UA="Mozilla/5.0 (compatible; OvechBot/1.0; +https://github.com/ovechbot) Chrome/120.0.0.0"

echo "=== Request (same as predictor) ==="
echo "GET $URL"
echo "User-Agent: $UA"
echo "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
echo ""

if [[ -n "$1" ]]; then
  echo "=== Saving full response to $1 ==="
  curl -sS -A "$UA" -H "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8" "$URL" -o "$1"
  body=$(cat "$1")
else
  echo "=== Fetching (no save) ==="
  body=$(curl -sS -A "$UA" -H "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8" "$URL")
fi

echo "=== Response length: ${#body} bytes ==="
echo ""

echo "=== Parser needs: Washington or Capitals + Montreal (or opponent) in same ~250 char window ==="
if echo "$body" | grep -qi "Washington"; then echo "  Found: Washington"; else echo "  MISSING: Washington"; fi
if echo "$body" | grep -qi "Capitals"; then echo "  Found: Capitals"; else echo "  MISSING: Capitals"; fi
if echo "$body" | grep -qi "Montreal"; then echo "  Found: Montreal"; else echo "  MISSING: Montreal"; fi
echo ""

echo "=== Parser looks for #N FirstName LastName (e.g. #79 Charlie Lindgren, #75 Jakub Dobes) ==="
echo "$body" | grep -oE '#[0-9]+\s+[A-Z][a-z]+\s+[A-Za-z\-]+' | sort -u | head -20
echo ""

echo "=== CONFIRMED / PROJECTED (status near goalie names) ==="
if echo "$body" | grep -qi "CONFIRMED"; then echo "  Found: CONFIRMED"; else echo "  MISSING: CONFIRMED"; fi
if echo "$body" | grep -qi "PROJECTED"; then echo "  Found: PROJECTED"; else echo "  MISSING: PROJECTED"; fi
echo ""

echo "=== Sample: lines containing Lindgren or Dobes ==="
echo "$body" | tr '>' '\n' | grep -iE 'Lindgren|Dobes' | head -10
echo ""

echo "=== Verdict ==="
if echo "$body" | grep -qi "Washington\|Capitals" && echo "$body" | grep -qi "Montreal" && echo "$body" | grep -qiE "Lindgren|Dobes|Charlie|Jakub"; then
  echo "  Page contains Caps + Montreal and goalie-like names — parser may succeed."
else
  echo "  Page may be JS-rendered or missing expected text — parser likely returns empty."
fi

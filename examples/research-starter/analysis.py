"""Summarize synthetic demonstration data; this is not a benchmark."""
import csv
from pathlib import Path
from statistics import mean, median, stdev

rows = list(csv.DictReader(Path("measurements.csv").open(newline="")))
latencies = [float(row["latency_ms"]) for row in rows]
successes = sum(int(row["success"]) for row in rows)
print("SYNTHETIC DEMO DATA — not a measured benchmark")
print(f"Trials: {len(rows)}")
print(f"Mean latency: {mean(latencies):.2f} ms")
print(f"Median latency: {median(latencies):.2f} ms")
print(f"Sample standard deviation: {stdev(latencies):.2f} ms")
print(f"Success rate: {successes / len(rows):.0%}")

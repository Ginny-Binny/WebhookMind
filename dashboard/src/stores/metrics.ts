import { createSignal } from 'solid-js';
import type { MetricPoint } from '../lib/types';

const [throughput, setThroughput] = createSignal<MetricPoint[]>([]);
const [latency, setLatency] = createSignal<MetricPoint[]>([]);

export function appendThroughputPoint(point: MetricPoint) {
  setThroughput((prev) => {
    const next = [...prev, point];
    if (next.length > 120) next.shift(); // keep 2 hours max
    return next;
  });
}

export function appendLatencyPoint(point: MetricPoint) {
  setLatency((prev) => {
    const next = [...prev, point];
    if (next.length > 120) next.shift();
    return next;
  });
}

export function setThroughputData(data: MetricPoint[]) {
  setThroughput(data);
}

export function setLatencyData(data: MetricPoint[]) {
  setLatency(data);
}

export { throughput, latency };

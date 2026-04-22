import { type Component, createEffect, onMount } from 'solid-js';
import * as d3 from 'd3';
import { latency, setLatencyData } from '../../stores/metrics';
import { fetchLatency } from '../../lib/api';

const LatencyChart: Component = () => {
  let svgRef!: SVGSVGElement;
  const margin = { top: 20, right: 20, bottom: 30, left: 50 };

  onMount(async () => {
    try {
      const data = await fetchLatency(60);
      setLatencyData(data);
    } catch { /* ignore */ }
  });

  createEffect(() => {
    const data = latency();
    if (!svgRef || data.length === 0) return;

    const width = svgRef.clientWidth - margin.left - margin.right;
    const height = svgRef.clientHeight - margin.top - margin.bottom;

    d3.select(svgRef).selectAll('*').remove();

    const svg = d3.select(svgRef)
      .append('g')
      .attr('transform', `translate(${margin.left},${margin.top})`);

    const x = d3.scaleTime()
      .domain(d3.extent(data, (d) => new Date(d.timestamp)) as [Date, Date])
      .range([0, width]);

    const y = d3.scaleLinear()
      .domain([0, d3.max(data, (d) => d.value) || 100])
      .nice()
      .range([height, 0]);

    svg.append('g').attr('transform', `translate(0,${height})`)
      .call(d3.axisBottom(x).ticks(6).tickFormat(d3.timeFormat('%H:%M') as any))
      .selectAll('text').attr('fill', '#6b7280').attr('font-size', '10');
    svg.append('g')
      .call(d3.axisLeft(y).ticks(5))
      .selectAll('text').attr('fill', '#6b7280').attr('font-size', '10');

    const line = d3.line<typeof data[0]>()
      .x((d) => x(new Date(d.timestamp)))
      .y((d) => y(d.value))
      .curve(d3.curveMonotoneX);

    svg.append('path')
      .datum(data)
      .attr('fill', 'none')
      .attr('stroke', '#22c55e')
      .attr('stroke-width', 2)
      .attr('d', line);

    svg.selectAll('.domain').remove();
  });

  return (
    <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
      <h3 class="text-sm font-semibold text-white mb-3">Latency (ms)</h3>
      <svg ref={svgRef} class="w-full" style={{ height: 'clamp(150px, 25vw, 200px)' }} />
    </div>
  );
};

export default LatencyChart;

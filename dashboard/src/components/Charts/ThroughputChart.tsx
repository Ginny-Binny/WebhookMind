import { type Component, createEffect, onMount } from 'solid-js';
import * as d3 from 'd3';
import { throughput } from '../../stores/metrics';
import { fetchThroughput } from '../../lib/api';
import { setThroughputData } from '../../stores/metrics';

const ThroughputChart: Component = () => {
  let svgRef!: SVGSVGElement;
  const margin = { top: 20, right: 20, bottom: 30, left: 50 };

  onMount(async () => {
    try {
      const data = await fetchThroughput(60);
      setThroughputData(data);
    } catch { /* ignore */ }
  });

  createEffect(() => {
    const data = throughput();
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
      .domain([0, d3.max(data, (d) => d.value) || 10])
      .nice()
      .range([height, 0]);

    // Grid lines
    svg.append('g').attr('class', 'grid')
      .call(d3.axisLeft(y).tickSize(-width).tickFormat(() => ''))
      .selectAll('line').attr('stroke', '#1e1e2e').attr('stroke-dasharray', '2,2');

    // Axes
    svg.append('g').attr('transform', `translate(0,${height})`)
      .call(d3.axisBottom(x).ticks(6).tickFormat(d3.timeFormat('%H:%M') as any))
      .selectAll('text').attr('fill', '#6b7280').attr('font-size', '10');
    svg.append('g')
      .call(d3.axisLeft(y).ticks(5))
      .selectAll('text').attr('fill', '#6b7280').attr('font-size', '10');

    // Line
    const line = d3.line<typeof data[0]>()
      .x((d) => x(new Date(d.timestamp)))
      .y((d) => y(d.value))
      .curve(d3.curveMonotoneX);

    svg.append('path')
      .datum(data)
      .attr('fill', 'none')
      .attr('stroke', '#3b82f6')
      .attr('stroke-width', 2)
      .attr('d', line);

    // Area fill
    const area = d3.area<typeof data[0]>()
      .x((d) => x(new Date(d.timestamp)))
      .y0(height)
      .y1((d) => y(d.value))
      .curve(d3.curveMonotoneX);

    svg.append('path')
      .datum(data)
      .attr('fill', 'url(#gradient)')
      .attr('d', area);

    // Gradient
    const defs = d3.select(svgRef).append('defs');
    const gradient = defs.append('linearGradient').attr('id', 'gradient').attr('x1', '0').attr('y1', '0').attr('x2', '0').attr('y2', '1');
    gradient.append('stop').attr('offset', '0%').attr('stop-color', '#3b82f6').attr('stop-opacity', 0.3);
    gradient.append('stop').attr('offset', '100%').attr('stop-color', '#3b82f6').attr('stop-opacity', 0);

    // Remove domain lines
    svg.selectAll('.domain').remove();
  });

  return (
    <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
      <h3 class="text-sm font-semibold text-white mb-3">Throughput (events/min)</h3>
      <svg ref={svgRef} class="w-full" style={{ height: 'clamp(150px, 25vw, 200px)' }} />
    </div>
  );
};

export default ThroughputChart;

import { useEffect, useRef } from 'react'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, PieChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

// ECharts 按需引入（包体积最小化）；图表配可读数字列表（无障碍与精度）。
echarts.use([BarChart, LineChart, PieChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

const COLORS = ['#2fa87c', '#8b7fd4', '#f2a35c', '#64a8e8', '#f2c94c', '#e0574b', '#5cc8be', '#b083f0', '#fb923c', '#94a3b8']

export function Chart({ option, height = 220 }: { option: echarts.EChartsCoreOption; height?: number }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    if (!ref.current) return
    const chart = echarts.init(ref.current)
    chartRef.current = chart
    const ro = new ResizeObserver(() => chart.resize())
    ro.observe(ref.current)
    return () => { ro.disconnect(); chart.dispose() }
  }, [])

  useEffect(() => {
    chartRef.current?.setOption({ color: COLORS, ...option }, true)
  }, [option])

  return <div ref={ref} style={{ width: '100%', height }} role="img" aria-label="统计图表" />
}

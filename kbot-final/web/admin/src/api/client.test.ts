import { AxiosError } from 'axios'
import { describe, expect, it } from 'vitest'

import { getAPIErrorMessage } from './client'

describe('getAPIErrorMessage', () => {
  it('reads the structured API error returned by the backend', () => {
    const error = new AxiosError('request failed', '400', undefined, undefined, {
      data: { error: '同一工作空间内名称已存在' },
      status: 409,
      statusText: 'Conflict',
      headers: {},
      config: { headers: {} } as never,
    })
    expect(getAPIErrorMessage(error)).toBe('同一工作空间内名称已存在')
  })

  it('keeps ordinary errors readable', () => {
    expect(getAPIErrorMessage(new Error('解析失败'))).toBe('解析失败')
  })
})

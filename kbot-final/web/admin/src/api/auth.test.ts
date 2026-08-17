import { AxiosError } from 'axios'
import { describe, expect, it } from 'vitest'

import { getLoginErrorMessage } from './auth'

describe('getLoginErrorMessage', () => {
  it('turns an authentication failure into a clear login hint', () => {
    const error = new AxiosError('request failed', '401', undefined, undefined, {
      data: 'invalid credentials\n',
      status: 401,
      statusText: 'Unauthorized',
      headers: {},
      config: { headers: {} } as never,
    })

    expect(getLoginErrorMessage(error)).toBe('邮箱或密码错误')
  })
})

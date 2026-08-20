import { Select, type SelectProps } from 'antd'
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  listModelDeployments,
  listModelProfiles,
  listModelProfileVersions,
} from '@/api/model'
import { useAuthStore } from '@/store/authStore'

type Props = Pick<SelectProps<string>, 'allowClear' | 'disabled' | 'placeholder' | 'style' | 'value'> & {
  onChange?: (value?: string) => void
}

export function ModelProfileVersionSelect({
  allowClear = true,
  disabled,
  onChange,
  placeholder = '选择 Model Profile Version',
  style,
  value,
}: Props) {
  const workspaceId = useAuthStore((state) => state.workspaceId)
  const profiles = useQuery({
    queryKey: ['model-profiles', workspaceId],
    queryFn: listModelProfiles,
    enabled: !!workspaceId,
  })
  const deployments = useQuery({
    queryKey: ['model-deployments', workspaceId],
    queryFn: listModelDeployments,
    enabled: !!workspaceId,
  })
  const versions = useQuery({
    queryKey: ['model-profile-versions', workspaceId],
    queryFn: listModelProfileVersions,
    enabled: !!workspaceId,
  })

  const options = useMemo(() => {
    const profileNames = new Map((profiles.data ?? []).map((item) => [item.id, item.name]))
    const deploymentByID = new Map((deployments.data ?? []).map((item) => [item.id, item]))
    return (versions.data ?? []).map((item) => {
      const deployment = deploymentByID.get(item.primary_deployment_id)
      const profileName = profileNames.get(item.profile_id) ?? item.profile_id.slice(0, 8)
      const deploymentName = deployment?.name ?? item.primary_deployment_id.slice(0, 8)
      const modelName = deployment?.model_name ?? '未知模型'
      return {
        value: item.id,
        label: `${profileName} / v${item.version} → ${deploymentName} / ${modelName} · ${item.classification_max}`,
      }
    })
  }, [deployments.data, profiles.data, versions.data])

  return (
    <Select<string>
      allowClear={allowClear}
      disabled={disabled}
      loading={profiles.isLoading || deployments.isLoading || versions.isLoading}
      notFoundContent="请先在模型配置中创建 Model Profile Version"
      onChange={(next) => onChange?.(next)}
      options={options}
      placeholder={placeholder}
      showSearch
      optionFilterProp="label"
      style={style}
      value={value}
    />
  )
}

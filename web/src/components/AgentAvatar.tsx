import React from 'react';
import { getAvatarUrl } from '../services/api';
import ConfigIcon from './ConfigIcon';
import type { IconType } from './ConfigIcon';

/* ===== AgentAvatar component ===== */

interface AgentAvatarProps {
  avatar: string;
  size?: number;
  iconSize?: number;
  borderRadius?: string;
}

const AgentAvatar: React.FC<AgentAvatarProps> = ({
  avatar,
  size = 44,
  iconSize = 20,
  borderRadius = '10px',
}) => {
  const avatarUrl = getAvatarUrl(avatar);

  if (avatarUrl) {
    return (
      <img
        src={avatarUrl}
        alt="avatar"
        style={{
          width: size,
          height: size,
          borderRadius,
          objectFit: 'cover',
        }}
      />
    );
  }

  return <ConfigIcon type="agent" size={size} iconSize={iconSize} borderRadius={borderRadius} />;
};

export type { IconType };
export default AgentAvatar;

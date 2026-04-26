-- Private Buddy 0.0.4 Schema
-- Export Date: 2026-04-26
-- Note: Add interactions table for agent-world interaction records
--       Add has_interactions field to messages table
--       Drop tasks table (task CRUD removed, agent execution integrated into chat)

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Table structure for table `interactions`
-- Stores agent-world interaction records (LLM request/response per iteration)
--

DROP TABLE IF EXISTS `interactions`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `interactions` (
  `id` int NOT NULL AUTO_INCREMENT,
  `session_id` int NOT NULL COMMENT 'Owning session',
  `user_msg_id` int NOT NULL COMMENT 'User message that triggered execution',
  `agent_msg_id` int NOT NULL COMMENT 'Agent message that delivers the result',
  `iteration` int NOT NULL COMMENT 'Iteration number within the execution',
  `type` int NOT NULL COMMENT '1=request (messages sent to LLM), 2=response (LLM output)',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Time of the interaction',
  `data` text COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'JSON: request=messages, response=content+tool_calls+finish_reason',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_interactions_session_user_agent_iter_type` (`session_id`, `user_msg_id`, `agent_msg_id`, `iteration`, `type`),
  KEY `idx_interactions_session` (`session_id`),
  KEY `idx_interactions_user_msg` (`user_msg_id`),
  KEY `idx_interactions_agent_msg` (`agent_msg_id`),
  KEY `idx_interactions_session_iteration` (`session_id`, `iteration`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Add has_interactions field to messages table
-- 0=pending (only agent messages), 1=has interactions, 2=no interactions
-- User messages always have has_interactions=2
--

ALTER TABLE `messages` ADD COLUMN `has_interactions` int NOT NULL DEFAULT 2 COMMENT '0=pending, 1=has interactions, 2=no interactions';

--
-- Drop tasks table (task CRUD module removed, agent execution integrated into chat)
--

DROP TABLE IF EXISTS `tasks`;

-- Create search_config table (single record, id=1)
CREATE TABLE IF NOT EXISTS search_config (
    id INTEGER PRIMARY KEY DEFAULT 1,
    provider VARCHAR(50) NOT NULL DEFAULT 'tavily',
    api_key VARCHAR(255) NOT NULL DEFAULT '',
    description TEXT DEFAULT '',
    is_active BOOLEAN DEFAULT 0,
    updated_at DATETIME
);

-- Insert default record
INSERT INTO search_config (id, provider, api_key, description, is_active)
VALUES (1, 'tavily', '', '', 0);

/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;
/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2026-04-26

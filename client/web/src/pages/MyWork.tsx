import { useEffect, useState } from "react";
import { Badge, Group, Stack, Table, Text, Title } from "@mantine/core";
import { api, type QueueItem } from "../api";

// The queue carries both kinds of submission, because "what should I do next"
// does not sort by where the work came from.
//
// It also carries two kinds of row: drafts nobody has submitted, and open steps
// FlowCore is holding. Only the second exists in the library — a draft is work
// the console knows about and FlowCore has never heard of.
export function MyWork() {
  const [items, setItems] = useState<QueueItem[] | null>(null);

  useEffect(() => {
    void api.queue().then(setItems);
  }, []);

  return (
    <Stack gap="md">
      <Title order={3}>My work</Title>

      {items === null ? (
        <Text c="dimmed">Loading…</Text>
      ) : items.length === 0 ? (
        <Text c="dimmed">
          Nothing is waiting on you. Switch to someone else from the account menu.
        </Text>
      ) : (
        <Table highlightOnHover verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Reference</Table.Th>
              <Table.Th>Type</Table.Th>
              <Table.Th>Waiting on</Table.Th>
              <Table.Th>Since</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {items.map((item) => (
              <Table.Tr key={item.reference}>
                <Table.Td>
                  <Text fw={500}>{item.reference}</Text>
                </Table.Td>
                <Table.Td>
                  <Badge variant="light" color={item.type === "claim" ? "blue" : "grape"}>
                    {item.type}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  {item.isDraft ? (
                    <Group gap="xs">
                      <Badge variant="outline" color="gray" size="sm">
                        draft
                      </Badge>
                      <Text size="sm" c="dimmed">
                        not yet submitted for assessment
                      </Text>
                    </Group>
                  ) : (
                    <Stack gap={0}>
                      <Text size="sm">{item.stepName}</Text>
                      <Text size="xs" c="dimmed">
                        {item.assignee}
                      </Text>
                    </Stack>
                  )}
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">
                    {new Date(item.waitingSince).toLocaleString()}
                  </Text>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </Stack>
  );
}

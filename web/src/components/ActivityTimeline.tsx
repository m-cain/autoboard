import type { ActivityEvent } from "@autoboard/contracts";

const timestamp = (value: string) => new Date(value).toLocaleString();

export const ActivityTimeline = ({
  activity,
}: {
  readonly activity: readonly ActivityEvent[];
}) => (
  <section className="detail-section" aria-labelledby="activity-heading">
    <h2 id="activity-heading">Activity</h2>
    {activity.length === 0 ? (
      <p className="empty-state">No activity yet</p>
    ) : (
      <ol className="activity-timeline">
        {activity.map((event) => (
          <li key={event.id}>
            <strong>{event.event_type}</strong>
            <span>{event.actor}</span>
            <time dateTime={event.inserted_at}>
              {timestamp(event.inserted_at)}
            </time>
          </li>
        ))}
      </ol>
    )}
  </section>
);

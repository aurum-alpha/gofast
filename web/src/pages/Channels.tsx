export function ChannelsPage() {
  return (
    <>
      <h1>Channels</h1>
      <p className="lead">
        Lineups will appear here after providers refresh. Nothing to show yet.
      </p>
      <div className="table-wrap">
        <table className="channels">
          <thead>
            <tr>
              <th scope="col">#</th>
              <th scope="col">Name</th>
              <th scope="col">Provider</th>
              <th scope="col">Class</th>
              <th scope="col">Export</th>
            </tr>
          </thead>
          <tbody>
            <tr className="empty">
              <td colSpan={5}>
                No channels yet — waiting on provider adapters and refresh.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </>
  )
}

import { Icon } from '../components/Icon';
import { useTitle } from '../hooks';
import { Link } from '../router';

export function NotFound({ what }: { what: string }) {
  useTitle('Not found · ccmeter');
  return (
    <div className="empty">
      <div className="missing">
        <Icon name="alert" />
        {what}
      </div>
      <Link className="btn" href="/">
        Back to the overview
      </Link>
    </div>
  );
}
